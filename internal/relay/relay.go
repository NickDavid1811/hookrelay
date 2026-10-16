// Package relay is the composition boundary for the executable and integration fixtures.
// Business use cases live in application; SQL in storage; HTTP in api/destination.
package relay

import (
	"context"
	"database/sql"
	"fmt"
	"hookrelay/internal/api"
	"hookrelay/internal/application"
	"hookrelay/internal/core"
	"hookrelay/internal/destination"
	"hookrelay/internal/observability"
	"hookrelay/internal/storage"
	"net/http"
	"sync"
	"time"
)

const MaxBody = core.MaxBody
const DefaultRetryAfterMax = core.DefaultRetryAfterMax

type Publication = core.Publication
type Result = core.Result
type Limits = core.Limits

var ID = core.ID
var Signature = destination.Signature
var Open = storage.Open
var OpenReader = storage.OpenReader
var OpenPool = storage.OpenPool
var DefaultLimits = core.DefaultLimits

// Service wires together the hexagonal components: application use cases, SQLite persistence,
// outbound destination transport, Prometheus telemetry, and the concurrent worker pool.
type Service struct {
	DB                     *sql.DB
	ReadDB                 *sql.DB
	Endpoints              map[string]endpointRuntime
	RetryAfterMax          time.Duration
	Limits                 Limits
	Workers                int
	MetricsToken           string
	metricsOnce            sync.Once
	AllowLoopbackEndpoints bool
	endpointsMu            sync.RWMutex
	observability          *observability.Metrics
}

func (s *Service) limits() Limits {
	if s.Limits.Queue == 0 {
		return DefaultLimits()
	}
	return s.Limits
}

// ValidateEndpoint checks URL policy without registering or mutating a destination.
func (s *Service) ValidateEndpoint(url string, allowLoopback bool) error {
	_, err := destination.Validate(url, allowLoopback)
	return err
}

// Initialize explicitly validates persisted dynamic destinations before serving or dispatching.
func (s *Service) Initialize(ctx context.Context) error {
	stored, err := (&storage.SQLite{DB: s.DB, ReadDB: s.ReadDB}).StoredEndpoints(ctx)
	if err != nil {
		return err
	}
	for _, ep := range stored {
		if ep.AllowLoopback && !s.AllowLoopbackEndpoints {
			return fmt.Errorf("stored test endpoint %s requires operator loopback permission", ep.ID)
		}
		if err := s.ValidateEndpoint(ep.URL, ep.AllowLoopback); err != nil {
			return fmt.Errorf("invalid stored endpoint %s: %w", ep.ID, err)
		}
	}
	return nil
}

// resolveEndpoint keeps configured secrets in memory; dynamic destinations come from committed SQLite rows.
func (s *Service) resolveEndpoint(ctx context.Context, id string) (destination.Runtime, bool, error) {
	s.endpointsMu.RLock()
	ep, ok := s.Endpoints[id]
	s.endpointsMu.RUnlock()
	if ok {
		return ep, true, nil
	}
	stored, err := (&storage.SQLite{DB: s.DB, ReadDB: s.ReadDB}).StoredEndpoint(ctx, id)
	if err != nil {
		return destination.Runtime{}, false, err
	}
	if stored.AllowLoopback && !s.AllowLoopbackEndpoints {
		return destination.Runtime{}, false, fmt.Errorf("loopback endpoint not permitted")
	}
	client, err := destination.Client(stored.URL, stored.AllowLoopback)
	if err != nil {
		return destination.Runtime{}, false, err
	}
	return destination.Runtime{URL: stored.URL, Secret: stored.Secret, Client: client}, true, nil
}

func (s *Service) application() *application.Service {
	store := &storage.SQLite{DB: s.DB, ReadDB: s.ReadDB}
	return &application.Service{
		Publications: store, ReplayStore: store, Queries: store, Queue: store, EndpointStore: store,
		Sender: destination.Sender{Resolve: s.resolveEndpoint}, Observer: s.metrics(), Policy: s,
		AllowLoopbackEndpoints: s.AllowLoopbackEndpoints,
		Limits:                 s.limits(), RetryAfterMax: s.RetryAfterMax, Workers: s.Workers,
	}
}

func (s *Service) Handler() http.Handler {
	server := &api.Server{
		Application: s.application(),
		Metrics:     http.HandlerFunc(s.scrape),
		ObserveResponse: func(operation string, code int) {
			s.metrics().Response(operation, code)
		},
	}
	return server.Handler()
}
func (s *Service) Run(ctx context.Context) error { return s.application().Run(ctx) }
func (s *Service) dispatchOne(ctx context.Context) (bool, error) {
	return s.application().DispatchOne(ctx)
}
func (s *Service) Cleanup(ctx context.Context, now time.Time) (int, error) {
	return s.application().Cleanup(ctx, now)
}
