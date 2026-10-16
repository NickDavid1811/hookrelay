// Package api adapts HTTP requests and errors to application use cases.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"hookrelay/internal/core"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type ownerKey struct{}

func owner(r *http.Request) string { value, _ := r.Context().Value(ownerKey{}).(string); return value }

// IngestionPort defines the primary driving port for publishing webhook events.
type IngestionPort interface {
	Publish(ctx context.Context, client, key string, body []byte) (core.Result, error)
}

// DeliveryPort defines the primary driving port for querying delivery status, history, and replay orchestration.
type DeliveryPort interface {
	Delivery(ctx context.Context, client, id string) (core.Result, error)
	Attempts(ctx context.Context, client, id string, after int) ([]core.AttemptRecord, error)
	Replay(ctx context.Context, client, id string, expected int, reason string) (core.Result, error)
	Replays(ctx context.Context, client, id string, after int) ([]core.ReplayRecord, error)
}

// EndpointPort defines the primary driving port for endpoint cataloging and dynamic provisioning.
type EndpointPort interface {
	CreateEndpoint(ctx context.Context, cmd core.CreateEndpointCommand) (core.EndpointCreated, error)
	GetEndpoint(ctx context.Context, client, id string) (core.EndpointRecord, error)
	DeleteEndpoint(ctx context.Context, client, id string) error
	Endpoints(ctx context.Context, client, after string) ([]core.EndpointRecord, error)
}

// AuthPort defines the primary driving port for client authentication and resource authorization.
type AuthPort interface {
	Authenticate(ctx context.Context, token string) (string, error)
	Owns(ctx context.Context, client, id string) error
}

// HealthPort defines the primary driving port for system health and readiness verification.
type HealthPort interface {
	Ping(ctx context.Context) error
}

// UseCases composes the primary driving ports exposed by the hexagonal application layer.
type UseCases interface {
	IngestionPort
	DeliveryPort
	EndpointPort
	AuthPort
	HealthPort
}

// Server provides the HTTP presentation adapter, handling routing, authentication middleware, and error translation.
type Server struct {
	Application     UseCases
	Metrics         http.Handler
	ObserveResponse func(string, int)
}

func reply(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, code int, message string) {
	reply(w, code, map[string]string{"error": message})
}
func respondError(w http.ResponseWriter, err error) {
	var business *core.Error
	if !errors.As(err, &business) {
		fail(w, 503, "storage unavailable")
		return
	}
	code := 503
	switch business.Kind {
	case core.Invalid:
		code = 400
	case core.Unauthorized:
		code = 401
	case core.NotFound:
		code = 404
	case core.Conflict:
		code = 409
	case core.ClientCapacity:
		code = 429
	}
	fail(w, code, business.Message)
}
func (s *Server) owns(w http.ResponseWriter, r *http.Request) bool {
	err := s.Application.Owns(r.Context(), owner(r), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return false
	}
	return true
}
func cursor(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		fail(w, 400, "after must be a nonnegative integer")
		return 0, false
	}
	return n, true
}

// Handler constructs and returns the configured http.Handler containing all API routes and middleware.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Application.Ping(r.Context()); err != nil {
			fail(w, 503, "storage unavailable")
			return
		}
		reply(w, 200, map[string]string{"status": "ready"})
	})
	if s.Metrics != nil {
		m.Handle("GET /metrics", s.Metrics)
	}
	m.HandleFunc("POST /api/v1/publish", s.publish)
	m.HandleFunc("POST /api/v1/deliveries/{id}/replay", s.replay)
	m.HandleFunc("GET /api/v1/deliveries/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Application.Delivery(r.Context(), owner(r), r.PathValue("id"))
		if err != nil {
			respondError(w, err)
			return
		}
		reply(w, 200, v)
	})
	m.HandleFunc("GET /api/v1/deliveries/{id}/attempts", s.attempts)
	m.HandleFunc("GET /api/v1/deliveries/{id}/replays", s.replays)
	m.HandleFunc("GET /api/v1/endpoints", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Application.Endpoints(r.Context(), owner(r), r.URL.Query().Get("after"))
		if err != nil {
			respondError(w, err)
			return
		}
		reply(w, 200, v)
	})
	m.HandleFunc("GET /api/v1/endpoints/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Application.GetEndpoint(r.Context(), owner(r), r.PathValue("id"))
		if err != nil {
			respondError(w, err)
			return
		}
		reply(w, 200, v)
	})
	m.HandleFunc("DELETE /api/v1/endpoints/{id}", func(w http.ResponseWriter, r *http.Request) {
		err := s.Application.DeleteEndpoint(r.Context(), owner(r), r.PathValue("id"))
		if err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	m.HandleFunc("POST /api/v1/endpoints", s.createEndpoint)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if operation := businessOperation(r); operation != "" && s.ObserveResponse != nil {
			recorder := &statusWriter{ResponseWriter: w}
			w = recorder
			defer func() { s.ObserveResponse(operation, recorder.code) }()
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				fail(w, 401, "unauthorized")
				return
			}
			client, err := s.Application.Authenticate(r.Context(), strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				respondError(w, err)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), ownerKey{}, client))
		}
		m.ServeHTTP(w, r)
	})
}
func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) == 0 || len(key) > 128 {
		fail(w, 400, "Idempotency-Key required, maximum 128 bytes")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, core.MaxBody))
	if err != nil {
		fail(w, 413, "body exceeds limit")
		return
	}
	v, err := s.Application.Publish(r.Context(), owner(r), key, body)
	if err != nil {
		respondError(w, err)
		return
	}
	reply(w, 202, v)
}
func (s *Server) replay(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedCycle int    `json:"expected_cycle"`
		Reason        string `json:"reason"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF || input.ExpectedCycle < 1 || len(input.Reason) > 256 {
		fail(w, 400, "expected_cycle required; reason maximum 256 bytes")
		return
	}
	v, err := s.Application.Replay(r.Context(), owner(r), r.PathValue("id"), input.ExpectedCycle, input.Reason)
	if err != nil {
		respondError(w, err)
		return
	}
	reply(w, 202, v)
}
func (s *Server) attempts(w http.ResponseWriter, r *http.Request) {
	after, ok := cursor(w, r)
	if !ok {
		return
	}
	v, err := s.Application.Attempts(r.Context(), owner(r), r.PathValue("id"), after)
	if err != nil {
		respondError(w, err)
		return
	}
	reply(w, 200, v)
}
func (s *Server) replays(w http.ResponseWriter, r *http.Request) {
	after, ok := cursor(w, r)
	if !ok {
		return
	}
	v, err := s.Application.Replays(r.Context(), owner(r), r.PathValue("id"), after)
	if err != nil {
		respondError(w, err)
		return
	}
	reply(w, 200, v)
}

func (s *Server) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID                string `json:"id"`
		URL               string `json:"url"`
		Secret            string `json:"secret"`
		AllowLoopback     bool   `json:"allow_loopback"`
		AllowLoopbackTest bool   `json:"allow_loopback_test"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, core.MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || dec.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid endpoint JSON payload")
		return
	}
	if input.URL == "" {
		fail(w, 400, "url is required")
		return
	}
	created, err := s.Application.CreateEndpoint(r.Context(), core.CreateEndpointCommand{
		ClientID:      owner(r),
		ID:            input.ID,
		URL:           input.URL,
		Secret:        input.Secret,
		AllowLoopback: input.AllowLoopback || input.AllowLoopbackTest,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	reply(w, 201, created)
}
