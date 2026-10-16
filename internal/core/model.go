// Package core defines domain models, business rules, and ports, independent of transport and persistence.
package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// MaxBody defines the maximum allowed size (64 KiB) for inbound publication bodies.
const MaxBody = 64 << 10

// ID generates a cryptographically secure 128-bit random hex identifier.
func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// TokenHash computes the SHA-256 hexadecimal digest of an authentication token for secure storage.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Kind classifies domain and business invariant violations.
type Kind string

const (
	// Invalid indicates malformed requests, invalid JSON, or syntax errors.
	Invalid Kind = "invalid"
	// Unauthorized indicates missing or invalid client authentication credentials.
	Unauthorized Kind = "unauthorized"
	// NotFound indicates that a requested delivery, event, or endpoint does not exist.
	NotFound Kind = "not_found"
	// Conflict indicates idempotency collisions, invalid state transitions, or mismatched replay cycles.
	Conflict Kind = "conflict"
	// ClientCapacity indicates that the authenticated client has exceeded their pending delivery quota.
	ClientCapacity Kind = "client_capacity"
	// Capacity indicates that global storage or queue limits have been reached.
	Capacity Kind = "capacity"
)

// Error represents a structured domain error containing an error classification Kind.
type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string { return e.Message }

// Failure constructs a new domain Error with the specified Kind and message.
func Failure(kind Kind, message string) error { return &Error{kind, message} }

// Publication represents the inbound webhook payload submitted by an authenticated client.
type Publication struct {
	EndpointID string          `json:"endpoint_id"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
}

// Result represents the public state and progress metrics of an enqueued delivery.
type Result struct {
	EventID       string `json:"event_id"`
	DeliveryID    string `json:"delivery_id"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	Cycle         int    `json:"cycle"`
	CycleAttempts int    `json:"cycle_attempts"`
	HTTPStatus    int    `json:"http_status"`
	Error         string `json:"error,omitempty"`
}

// PublishCommand contains the parameters required to atomically persist an event and delivery.
type PublishCommand struct {
	Client, Key, Hash   string
	Publication         Publication
	EventID, DeliveryID string
	Created             int64
}

// ReplayCommand encapsulates the request parameters to re-enqueue a DEAD delivery.
type ReplayCommand struct {
	Client, ID    string
	ExpectedCycle int
	Reason        string
	Requested     int64
}

// Claim represents an atomic lease acquisition by a worker for dispatch execution.
type Claim struct {
	ID, EventID, EndpointID, Token string
	Attempts, Cycle, CycleAttempts int
}

// Completion contains the evaluated delivery outcome to be atomically finalized in storage.
type Completion struct {
	Claim                     Claim
	Status                    string
	Code                      int
	Error                     string
	Next, Terminal, Completed int64
}

// Outbound represents the HTTP request payload and signature headers dispatched to a destination.
type Outbound struct {
	EventID, DeliveryID, EndpointID string
	Body                            []byte
	Timestamp                       string
}

// DeliveryState represents the lifecycle state of a webhook delivery.
type DeliveryState string

const (
	StatusPending    DeliveryState = "PENDING"
	StatusProcessing DeliveryState = "PROCESSING"
	StatusRetrying   DeliveryState = "RETRYING"
	StatusSuccess    DeliveryState = "SUCCESS"
	StatusDead       DeliveryState = "DEAD"
)

// HTTPResult captures the raw network outcome, underlying error, and headers received from the destination.
type HTTPResult struct {
	Code              int
	Error, RetryAfter string
	Err               error
	Duration          time.Duration
}

// EndpointSetup defines static configuration for an endpoint from configuration files.
type EndpointSetup struct {
	ID, URL, SecretEnv string
	AllowLoopback      bool
}

// ClientSetup defines static configuration for a client and their assigned endpoints.
type ClientSetup struct {
	ID, TokenHash string
	Endpoints     []EndpointSetup
}

// AttemptRecord contains the historical execution metrics of a single delivery attempt.
type AttemptRecord struct {
	Number          int    `json:"number"`
	Cycle           int    `json:"cycle"`
	HTTPStatus      int    `json:"http_status"`
	Error           string `json:"error"`
	CompletedUnixMs int64  `json:"completed_unix_ms"`
}

// CompletedAt returns the completion timestamp as a standard time.Time.
func (a AttemptRecord) CompletedAt() time.Time {
	return time.UnixMilli(a.CompletedUnixMs)
}

// ReplayRecord contains the audit trail information for a manual replay request.
type ReplayRecord struct {
	Cycle           int    `json:"cycle"`
	Actor           string `json:"actor"`
	Reason          string `json:"reason"`
	RequestedUnixMs int64  `json:"requested_unix_ms"`
}

// RequestedAt returns the request timestamp as a standard time.Time.
func (r ReplayRecord) RequestedAt() time.Time {
	return time.UnixMilli(r.RequestedUnixMs)
}

// EndpointRecord represents the basic identity and URL of an endpoint for client listings.
type EndpointRecord struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// CreateEndpointCommand encapsulates the parameters to dynamically create and persist an endpoint.
type CreateEndpointCommand struct {
	ClientID      string
	ID            string
	URL           string
	Secret        string
	AllowLoopback bool
}

// EndpointCreated represents the confirmation response for a newly created dynamic endpoint.
type EndpointCreated struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// StoredEndpoint represents an endpoint retrieved from persistent storage during startup or lookup.
type StoredEndpoint struct {
	ID            string
	URL           string
	Secret        string
	AllowLoopback bool
}

// PublicationStore defines the persistence port for atomic event ingestion.
type PublicationStore interface {
	Publish(context.Context, PublishCommand, Limits) (Result, bool, error)
}

// ReplayStore defines the persistence port for re-enqueuing DEAD deliveries with an audit record.
type ReplayStore interface {
	Replay(context.Context, ReplayCommand, Limits) (Result, error)
}

// QueryStore defines read-only persistence ports for delivery status, history, and client queries.
type QueryStore interface {
	Authenticate(context.Context, string) (string, error)
	Ping(context.Context) error
	Owns(context.Context, string, string) error
	Delivery(context.Context, string, string) (Result, error)
	Attempts(context.Context, string, string, int) ([]AttemptRecord, error)
	Replays(context.Context, string, string, int) ([]ReplayRecord, error)
	Endpoints(context.Context, string, string) ([]EndpointRecord, error)
}

// QueueStore defines the worker dispatch persistence port: claiming, payload retrieval, finalization, and cleanup.
type QueueStore interface {
	Claim(context.Context, int64, string) (Claim, bool, error)
	Payload(context.Context, string) ([]byte, error)
	Finalize(context.Context, Completion) error
	Cleanup(context.Context, time.Time, Limits) (int, error)
}

// EndpointStore defines the persistence port for dynamic endpoint creation, retrieval, and deactivation.
type EndpointStore interface {
	CreateEndpoint(context.Context, CreateEndpointCommand, Limits) (EndpointCreated, error)
	GetEndpoint(ctx context.Context, client, id string) (EndpointRecord, error)
	DeleteEndpoint(ctx context.Context, client, id string) error
	StoredEndpoints(context.Context) ([]StoredEndpoint, error)
	StoredEndpoint(context.Context, string) (StoredEndpoint, error)
}

// Store aggregates all outbound persistence ports for complete database implementations.
type Store interface {
	PublicationStore
	ReplayStore
	QueryStore
	QueueStore
	EndpointStore
}

// Sender defines the outbound HTTP transport port for dispatching webhooks to destinations.
type Sender interface {
	Send(context.Context, Outbound) (HTTPResult, error)
}

// Observer defines the outbound telemetry port for publishing metrics, counters, and runtime events.
type Observer interface {
	Publication(string)
	HTTP(HTTPResult)
	Delivery(string)
	Cleanup(error, int, time.Time)
	Worker(bool)
}

// NopObserver provides a no-op implementation of Observer when telemetry is disabled.
type NopObserver struct{}

func (NopObserver) Publication(string)            {}
func (NopObserver) HTTP(HTTPResult)               {}
func (NopObserver) Delivery(string)               {}
func (NopObserver) Cleanup(error, int, time.Time) {}
func (NopObserver) Worker(bool)                   {}

// CapacityStats encapsulates storage and queue metric counters for admission evaluation.
type CapacityStats struct {
	GlobalQueue  int
	TenantQueue  int
	TotalEvents  int
	PayloadBytes int64
	NewPayload   int
	IsNewEvent   bool
}

// CapacityError evaluates whether an inbound publication violates global or client quotas.
func CapacityError(stats CapacityStats, l Limits) error {
	if stats.TenantQueue >= l.ClientQueue {
		return Failure(ClientCapacity, "client queue capacity reached")
	}
	if stats.GlobalQueue >= l.Queue || (stats.IsNewEvent && (stats.TotalEvents >= l.Events || int64(stats.NewPayload) > l.PayloadBytes || stats.PayloadBytes > l.PayloadBytes-int64(stats.NewPayload))) {
		return Failure(Capacity, "storage or global queue capacity reached")
	}
	return nil
}

// ReplayAllowed checks whether a delivery qualifies for manual replay against cycle limits.
func ReplayAllowed(status string, cycle, expected, max int) error {
	if status != string(StatusDead) || cycle != expected {
		return Failure(Conflict, "replay requires DEAD and matching expected_cycle")
	}
	if cycle >= max {
		return Failure(Conflict, "maximum delivery cycles reached")
	}
	return nil
}

// DeliveryStatus classifies the resulting delivery state (SUCCESS, RETRYING, DEAD)
// based on HTTP status code, transport errors, attempt count, and configured maximum attempts.
func DeliveryStatus(code int, reason string, attempts int, maxAttempts int) string {
	if code >= 200 && code < 300 && reason == "" {
		return string(StatusSuccess)
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if attempts+1 < maxAttempts && (code == 0 || code == 408 || code == 429 || (code >= 500 && code <= 599)) {
		return string(StatusRetrying)
	}
	return string(StatusDead)
}

// StorageStats contains neutral aggregate counts; the monitoring adapter supplies the Prometheus prefix.
type StorageStats struct{ States, Values map[string]float64 }

// StatisticsSource defines the port for extracting consistent storage metrics snapshots.
type StatisticsSource interface {
	Snapshot(context.Context, time.Time, Limits) (StorageStats, error)
}
