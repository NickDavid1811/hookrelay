// Package application orchestrates use cases through ports, without infrastructure imports.
package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"hookrelay/internal/core"
	"io"
	"strconv"
	"sync"
	"time"
)

// EndpointPolicy validates destinations without changing runtime or persistent state.
type EndpointPolicy interface {
	ValidateEndpoint(url string, allowLoopback bool) error
}

// Service coordinates business use cases by consuming core ports without infrastructure coupling.
type Service struct {
	Publications           core.PublicationStore
	ReplayStore            core.ReplayStore
	Queries                core.QueryStore
	Queue                  core.QueueStore
	EndpointStore          core.EndpointStore
	Sender                 core.Sender
	Observer               core.Observer
	Policy                 EndpointPolicy
	AllowLoopbackEndpoints bool
	Limits                 core.Limits
	RetryAfterMax          time.Duration
	Workers                int
}

func (s *Service) workers() int {
	if s.Workers <= 0 {
		return 1
	}
	return s.Workers
}

func (s *Service) observer() core.Observer {
	if s.Observer == nil {
		return core.NopObserver{}
	}
	return s.Observer
}
func (s *Service) limits() core.Limits {
	if s.Limits.Queue == 0 {
		return core.DefaultLimits()
	}
	return s.Limits
}

// Authenticate verifies the client token length and retrieves the authenticated client ID from storage.
func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	if len(token) < 16 || len(token) > 4096 {
		return "", core.Failure(core.Unauthorized, "unauthorized")
	}
	return s.Queries.Authenticate(ctx, core.TokenHash(token))
}

func parsePublication(body []byte) (core.Publication, error) {
	var p core.Publication
	if len(body) > core.MaxBody {
		return p, core.Failure(core.Invalid, "invalid publication")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || dec.Decode(new(any)) != io.EOF {
		return p, core.Failure(core.Invalid, "invalid publication")
	}
	if p.EndpointID == "" || p.EventType == "" || len(p.EventType) > 128 || len(p.Payload) == 0 || !json.Valid(p.Payload) {
		return p, core.Failure(core.Invalid, "invalid publication")
	}
	return p, nil
}

// Publish coordinates the atomic ingestion of an inbound event and enqueues a new delivery.
// Idempotency is strictly enforced by comparing the payload hash against previous publications.
func (s *Service) Publish(ctx context.Context, client, key string, body []byte) (core.Result, error) {
	if len(key) == 0 || len(key) > 128 {
		return core.Result{}, core.Failure(core.Invalid, "Idempotency-Key required, maximum 128 bytes")
	}
	p, err := parsePublication(body)
	if err != nil {
		return core.Result{}, err
	}
	command := core.PublishCommand{
		Client:      client,
		Key:         key,
		Hash:        core.TokenHash(string(body)),
		Publication: p,
		EventID:     core.ID(),
		DeliveryID:  core.ID(),
		Created:     time.Now().UnixMilli(),
	}
	result, idempotent, err := s.Publications.Publish(ctx, command, s.limits())
	if err == nil {
		value := "new"
		if idempotent {
			value = "idempotent"
		}
		s.observer().Publication(value)
	}
	return result, err
}

// Replay re-enqueues a DEAD delivery into a new attempt cycle, recording an audit entry.
func (s *Service) Replay(ctx context.Context, client, id string, expected int, reason string) (core.Result, error) {
	if expected < 1 || len(reason) > 256 {
		return core.Result{}, core.Failure(core.Invalid, "expected_cycle required; reason maximum 256 bytes")
	}
	return s.ReplayStore.Replay(ctx, core.ReplayCommand{Client: client, ID: id, ExpectedCycle: expected, Reason: reason, Requested: time.Now().UnixMilli()}, s.limits())
}

// Cleanup executes a retention sweep deleting terminal events that have exceeded retention periods.
func (s *Service) Cleanup(ctx context.Context, now time.Time) (int, error) {
	n, err := s.Queue.Cleanup(ctx, now, s.limits())
	s.observer().Cleanup(err, n, time.Now())
	return n, err
}

// DispatchOne claims an eligible delivery via an atomic lease, executes HTTP dispatch,
// evaluates retry policies, and finalizes the delivery state in storage.
func (s *Service) DispatchOne(ctx context.Context) (bool, error) {
	claim, found, err := s.Queue.Claim(ctx, time.Now().UnixMilli(), core.ID())
	if err != nil || !found {
		return found, err
	}
	body, err := s.Queue.Payload(ctx, claim.EventID)
	if err != nil {
		return true, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := s.Sender.Send(requestCtx, core.Outbound{EventID: claim.EventID, DeliveryID: claim.ID, EndpointID: claim.EndpointID, Body: body, Timestamp: strconv.FormatInt(time.Now().Unix(), 10)})
	if err != nil {
		return true, err
	}
	s.observer().HTTP(result)
	status := core.DeliveryStatus(result.Code, result.Error, claim.CycleAttempts, s.limits().MaxAttempts)
	delay := core.BackoffCeiling(claim.CycleAttempts)
	var b [1]byte
	rand.Read(b[:])
	delay = time.Duration(float64(delay) * float64(b[0]) / 255)
	finished := time.Now()
	if status == string(core.StatusRetrying) {
		delay = core.RetryDelay(result.Code, result.RetryAfter, finished, delay, s.RetryAfterMax)
	}
	terminal := int64(0)
	if status == string(core.StatusSuccess) || status == string(core.StatusDead) {
		terminal = finished.UnixMilli()
	}
	err = s.Queue.Finalize(ctx, core.Completion{Claim: claim, Status: status, Code: result.Code, Error: result.Error, Next: finished.Add(delay).UnixMilli(), Terminal: terminal, Completed: time.Now().UnixMilli()})
	if err == nil {
		s.observer().Delivery(status)
	}
	return true, err
}
func (s *Service) Run(ctx context.Context) error {
	s.observer().Worker(true)
	defer s.observer().Worker(false)

	numWorkers := s.workers()
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	errCh := make(chan error, numWorkers+1)
	var wg sync.WaitGroup

	cleanup := time.NewTicker(s.limits().CleanupInterval)
	defer cleanup.Stop()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-cleanup.C:
				if _, err := s.Cleanup(workerCtx, time.Now()); err != nil {
					if workerCtx.Err() == nil {
						select {
						case errCh <- err:
						default:
						}
						cancelWorkers()
					}
					return
				}
			}
		}
	}()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if workerCtx.Err() != nil {
					return
				}
				processed, err := s.DispatchOne(workerCtx)
				if err != nil {
					if workerCtx.Err() == nil {
						select {
						case errCh <- err:
						default:
						}
						cancelWorkers()
					}
					return
				}
				if processed {
					continue
				}
				idle := time.NewTimer(100 * time.Millisecond)
				select {
				case <-workerCtx.Done():
					idle.Stop()
					return
				case <-idle.C:
				}
			}
		}()
	}

	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// Ping checks database connectivity.
func (s *Service) Ping(ctx context.Context) error { return s.Queries.Ping(ctx) }

// Owns checks whether the authenticated client owns the specified delivery.
func (s *Service) Owns(ctx context.Context, client, id string) error {
	return s.Queries.Owns(ctx, client, id)
}

// Delivery returns the current state and latest attempt result for a delivery.
func (s *Service) Delivery(ctx context.Context, client, id string) (core.Result, error) {
	return s.Queries.Delivery(ctx, client, id)
}

// Attempts returns a paginated list of historical execution attempts for a delivery.
func (s *Service) Attempts(ctx context.Context, client, id string, after int) ([]core.AttemptRecord, error) {
	return s.Queries.Attempts(ctx, client, id, after)
}

// Replays returns a paginated audit trail of replay requests for a delivery.
func (s *Service) Replays(ctx context.Context, client, id string, after int) ([]core.ReplayRecord, error) {
	return s.Queries.Replays(ctx, client, id, after)
}

// Endpoints returns all active endpoints associated with the authenticated client.
func (s *Service) Endpoints(ctx context.Context, client, after string) ([]core.EndpointRecord, error) {
	return s.Queries.Endpoints(ctx, client, after)
}

// CreateEndpoint validates the operator policy and atomically persists an immutable destination.
func (s *Service) CreateEndpoint(ctx context.Context, cmd core.CreateEndpointCommand) (core.EndpointCreated, error) {
	if cmd.ClientID == "" {
		return core.EndpointCreated{}, core.Failure(core.Unauthorized, "unauthorized")
	}
	if cmd.ID == "" {
		cmd.ID = "ep_" + core.ID()[:16]
	}
	if len(cmd.ID) > 64 {
		return core.EndpointCreated{}, core.Failure(core.Invalid, "endpoint ID exceeds maximum 64 bytes")
	}
	for _, c := range cmd.ID {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return core.EndpointCreated{}, core.Failure(core.Invalid, "endpoint ID contains invalid characters")
		}
	}
	if cmd.Secret == "" {
		cmd.Secret = "hrsec_" + core.ID() + core.ID()
	}
	if len(cmd.Secret) < 16 || len(cmd.Secret) > 4096 {
		return core.EndpointCreated{}, core.Failure(core.Invalid, "secret must contain 16..4096 bytes")
	}

	if cmd.AllowLoopback && !s.AllowLoopbackEndpoints {
		return core.EndpointCreated{}, core.Failure(core.Invalid, "loopback endpoints require operator test permission")
	}
	if s.Policy == nil {
		return core.EndpointCreated{}, errors.New("endpoint policy unavailable")
	}
	if err := s.Policy.ValidateEndpoint(cmd.URL, cmd.AllowLoopback); err != nil {
		return core.EndpointCreated{}, core.Failure(core.Invalid, err.Error())
	}
	return s.EndpointStore.CreateEndpoint(ctx, cmd, s.limits())
}

// GetEndpoint retrieves a single active endpoint owned by the authenticated client.
func (s *Service) GetEndpoint(ctx context.Context, client, id string) (core.EndpointRecord, error) {
	if client == "" {
		return core.EndpointRecord{}, core.Failure(core.Unauthorized, "unauthorized")
	}
	if id == "" {
		return core.EndpointRecord{}, core.Failure(core.Invalid, "endpoint id required")
	}
	return s.EndpointStore.GetEndpoint(ctx, client, id)
}

// DeleteEndpoint deactivates an endpoint only when no unfinished deliveries reference it.
func (s *Service) DeleteEndpoint(ctx context.Context, client, id string) error {
	if client == "" {
		return core.Failure(core.Unauthorized, "unauthorized")
	}
	if id == "" {
		return core.Failure(core.Invalid, "endpoint id required")
	}
	if err := s.EndpointStore.DeleteEndpoint(ctx, client, id); err != nil {
		return err
	}
	return nil
}
