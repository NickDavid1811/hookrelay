package relay

import (
	"context"
	"errors"
	"hookrelay/internal/core"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func expectKind(t *testing.T, err error, kind core.Kind) {
	t.Helper()
	var e *core.Error
	if !errors.As(err, &e) || e.Kind != kind {
		t.Fatalf("expected %s, got %v", kind, err)
	}
}
func addReviewClient(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.DB.Exec("INSERT INTO clients(id,token_hash,active) VALUES('other',?,1)", core.TokenHash("other-tenant-test-token")); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedCreationCannotChangeAnotherTenantDestination(t *testing.T) {
	s, _ := setup(t)
	addReviewClient(t, s)
	s.AllowLoopbackEndpoints = true
	var original, redirected atomic.Int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		original.Add(1)
		w.WriteHeader(204)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(204) }))
	defer bad.Close()
	s.testEndpoint(t, good.URL)
	h := s.Handler()
	r := httptest.NewRequest("POST", "/api/v1/endpoints", strings.NewReader(`{"id":"test","url":"`+bad.URL+`","allow_loopback":true}`))
	r.Header.Set("Authorization", "Bearer other-tenant-test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	if w = publish(s, "victim", body); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	if _, err := s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if original.Load() != 1 || redirected.Load() != 0 {
		t.Fatalf("original=%d redirected=%d", original.Load(), redirected.Load())
	}
}
func TestLoopbackRequiresOperatorPermission(t *testing.T) {
	s, _ := setup(t)
	app := s.application()
	cmd := core.CreateEndpointCommand{ClientID: "local-client", ID: "loop", URL: "http://127.0.0.1:9999", AllowLoopback: true}
	_, err := app.CreateEndpoint(context.Background(), cmd)
	expectKind(t, err, core.Invalid)
	s.AllowLoopbackEndpoints = true
	if _, err = s.application().CreateEndpoint(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	s.AllowLoopbackEndpoints = false
	if err = s.Initialize(context.Background()); err == nil {
		t.Fatal("persisted loopback endpoint silently enabled")
	}
}
func TestEndpointPersistenceFailureLeavesNoRuntimeState(t *testing.T) {
	s, _ := setup(t)
	if _, err := s.DB.Exec("CREATE TRIGGER reject_endpoint BEFORE INSERT ON endpoints BEGIN SELECT RAISE(ABORT,'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	_, err := s.application().CreateEndpoint(context.Background(), core.CreateEndpointCommand{ClientID: "local-client", ID: "failed", URL: "https://example.com/hook"})
	if err == nil {
		t.Fatal("expected insertion failure")
	}
	if _, _, err = s.resolveEndpoint(context.Background(), "failed"); err == nil {
		t.Fatal("failed destination still resolves")
	}
	if len(s.Endpoints) != 1 {
		t.Fatal("runtime map mutated")
	}
}
func TestConcurrentEndpointCreationPreservesCommittedOwner(t *testing.T) {
	s, _ := setup(t)
	addReviewClient(t, s)
	app := s.application()
	start := make(chan struct{})
	out := make(chan core.EndpointCreated, 2)
	fail := make(chan error, 2)
	for _, client := range []string{"local-client", "other"} {
		go func(client string) {
			<-start
			ep, err := app.CreateEndpoint(context.Background(), core.CreateEndpointCommand{ClientID: client, ID: "same", URL: "https://" + client + ".example.com/hook"})
			if err != nil {
				fail <- err
			} else {
				out <- ep
			}
		}(client)
	}
	close(start)
	var winner core.EndpointCreated
	for i := 0; i < 2; i++ {
		select {
		case winner = <-out:
		case err := <-fail:
			expectKind(t, err, core.Conflict)
		case <-time.After(3 * time.Second):
			t.Fatal("create blocked")
		}
	}
	ep, ok, err := s.resolveEndpoint(context.Background(), "same")
	if err != nil || !ok || ep.URL != winner.URL || ep.Secret != winner.Secret {
		t.Fatal(ok, err, "destination disagrees with committed winner")
	}
}

type deleteDuringPayload struct {
	core.QueueStore
	delete func() error
	t      *testing.T
}

func (q deleteDuringPayload) Payload(ctx context.Context, id string) ([]byte, error) {
	expectKind(q.t, q.delete(), core.Conflict)
	return q.QueueStore.Payload(ctx, id)
}
func TestDeleteRejectsUnfinishedWorkBeforeAndAfterClaim(t *testing.T) {
	s, _ := setup(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.WriteHeader(204) }))
	defer receiver.Close()
	s.AllowLoopbackEndpoints = true
	app := s.application()
	if _, err := app.CreateEndpoint(context.Background(), core.CreateEndpointCommand{
		ClientID: "local-client", ID: "delete-race-ep", URL: receiver.URL, AllowLoopback: true,
	}); err != nil {
		t.Fatal(err)
	}
	publication := strings.Replace(body, `"endpoint_id":"test"`, `"endpoint_id":"delete-race-ep"`, 1)
	if w := publish(s, "delete-race", publication); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	expectKind(t, app.DeleteEndpoint(context.Background(), "local-client", "delete-race-ep"), core.Conflict)
	app.Queue = deleteDuringPayload{QueueStore: app.Queue, t: t, delete: func() error { return app.DeleteEndpoint(context.Background(), "local-client", "delete-race-ep") }}
	if found, err := app.DispatchOne(context.Background()); err != nil || !found {
		t.Fatal(found, err)
	}
	if err := app.DeleteEndpoint(context.Background(), "local-client", "delete-race-ep"); err != nil {
		t.Fatal(err)
	}
}
func TestEndpointQuotasAndTombstoneCleanup(t *testing.T) {
	s, _ := setup(t)
	addReviewClient(t, s)
	s.Limits = DefaultLimits()
	s.Limits.Endpoints = 3
	s.Limits.ClientEndpoints = 2
	app := s.application()
	ctx := context.Background()
	create := func(client, id string) error {
		_, err := app.CreateEndpoint(ctx, core.CreateEndpointCommand{ClientID: client, ID: id, URL: "https://example.com/hook"})
		return err
	}
	if err := create("local-client", "a"); err != nil {
		t.Fatal(err)
	}
	expectKind(t, create("local-client", "a2"), core.ClientCapacity)
	if err := create("other", "b"); err != nil {
		t.Fatal(err)
	}
	expectKind(t, create("other", "b2"), core.Capacity)
	if err := app.DeleteEndpoint(ctx, "local-client", "a"); err != nil {
		t.Fatal(err)
	}
	expectKind(t, create("local-client", "a2"), core.ClientCapacity)
	if _, err := app.Cleanup(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := create("local-client", "a2"); err != nil {
		t.Fatal("unused tombstone capacity not reclaimed", err)
	}
}
func TestConcurrentEndpointAdmissionIsBounded(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.ClientEndpoints = 3
	app := s.application()
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := app.CreateEndpoint(context.Background(), core.CreateEndpointCommand{ClientID: "local-client", URL: "https://example.com/hook"})
			if err == nil {
				accepted.Add(1)
			} else {
				var e *core.Error
				if !errors.As(err, &e) || e.Kind != core.ClientCapacity {
					t.Errorf("unexpected rejection: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("accepted %d beyond one existing static endpoint", accepted.Load())
	}
}
func TestInitializationFailsExplicitly(t *testing.T) {
	s, _ := setup(t)
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("INSERT INTO endpoints(id,client_id,url,secret_env,allow_loopback,active,secret) VALUES('invalid','local-client','http://127.0.0.1:1','',0,1,'stored-secret-test')"); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err == nil {
		t.Fatal("invalid persisted destination ignored")
	}
}

func TestDeletedDynamicEndpointKeepsHistoryUntilRetention(t *testing.T) {
	s, _ := setup(t)
	s.AllowLoopbackEndpoints = true
	s.Limits = DefaultLimits()
	s.Limits.ClientEndpoints = 2
	s.Limits.SuccessRetention = time.Second
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.WriteHeader(204) }))
	defer receiver.Close()
	app := s.application()
	ctx := context.Background()
	if _, err := app.CreateEndpoint(ctx, core.CreateEndpointCommand{ClientID: "local-client", ID: "history", URL: receiver.URL, AllowLoopback: true}); err != nil {
		t.Fatal(err)
	}
	result, err := app.Publish(ctx, "local-client", "history-key", []byte(`{"endpoint_id":"history","event_type":"test","payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.DispatchOne(ctx); err != nil {
		t.Fatal(err)
	}
	if err = app.DeleteEndpoint(ctx, "local-client", "history"); err != nil {
		t.Fatal(err)
	}
	if n, err := app.Cleanup(ctx, time.Now()); err != nil || n != 0 {
		t.Fatal("history removed early", n, err)
	}
	if _, err = app.Delivery(ctx, "local-client", result.DeliveryID); err != nil {
		t.Fatal("terminal history lost", err)
	}
	_, err = app.CreateEndpoint(ctx, core.CreateEndpointCommand{ClientID: "local-client", ID: "next", URL: "https://example.com"})
	expectKind(t, err, core.ClientCapacity)
	if n, err := app.Cleanup(ctx, time.Now().Add(2*time.Second)); err != nil || n != 1 {
		t.Fatal("history not reclaimed", n, err)
	}
	if _, err = app.CreateEndpoint(ctx, core.CreateEndpointCommand{ClientID: "local-client", ID: "next", URL: "https://example.com"}); err != nil {
		t.Fatal("endpoint quota not reclaimed", err)
	}
	var violations int
	if err = s.DB.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations != 0 {
		t.Fatal(violations, err)
	}
}
