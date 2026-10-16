package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"hookrelay/internal/destination"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func setup(t *testing.T) (*Service, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.db")
	db, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := &Service{DB: db}
	cfg := Config{Clients: []ClientConfig{{ID: "local-client", TokenEnv: "TEST_TOKEN", Endpoints: []EndpointConfig{{ID: "test", URL: "http://127.0.0.1:1", SecretEnv: "TEST_SECRET", AllowLoopback: true}}}}}
	if err := s.ApplyConfig(cfg, func(name string) string {
		if name == "TEST_TOKEN" {
			return testToken
		}
		return testSecret
	}); err != nil {
		t.Fatal(err)
	}
	return s, p
}
func publish(s *Service, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/publish", bytes.NewBufferString(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

const body = `{"endpoint_id":"test","event_type":"invoice.paid","payload":{"invoice_id":"inv_1"}}`

func TestVerticalAndPersistence(t *testing.T) {
	s, path := setup(t)
	var received atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if Signature(testSecret, r.Header.Get("X-Webhook-Timestamp"), b) != r.Header.Get("X-Webhook-Signature") {
			t.Error("signature mismatch")
		}
		if string(b) != `{"invoice_id":"inv_1"}` || r.Header.Get("X-Webhook-Delivery-Id") == "" {
			t.Error("body or identifiers")
		}
		received.Add(1)
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	s.testEndpoint(t, receiver.URL)
	w := publish(s, "key", body)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var result Result
	json.Unmarshal(w.Body.Bytes(), &result)
	if err := s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if received.Load() != 1 {
		t.Fatal("not received")
	}
	s.DB.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB = db
	defer db.Close()
	r := httptest.NewRequest("GET", "/api/v1/deliveries/"+result.DeliveryID, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	json.Unmarshal(out.Body.Bytes(), &result)
	if result.Status != "SUCCESS" || result.Attempts != 1 || result.HTTPStatus != 204 {
		t.Fatal(out.Body)
	}
}
func TestIdempotencyAtomicityAndAuth(t *testing.T) {
	s, _ := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := publish(s, "same", body); w.Code != 202 {
				t.Errorf("publish %d", w.Code)
			}
		}()
	}
	wg.Wait()
	var n int
	s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	if publish(s, "same", body+" ").Code != 409 {
		t.Fatal("missing conflict")
	}
	r := httptest.NewRequest("GET", "/api/v1/deliveries/anything", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	s.DB.Exec(`CREATE TRIGGER reject_delivery BEFORE INSERT ON deliveries BEGIN SELECT RAISE(ABORT,'test'); END`)
	if publish(s, "failure", body).Code != 503 {
		t.Fatal("false acceptance")
	}
	s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 1 {
		t.Fatal("partial transaction")
	}
}
func TestRetryExhaustionRecoveryAndRedirect(t *testing.T) {
	for _, code := range []int{503, 400, 302} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			s, _ := setup(t)
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "http://127.0.0.1:1")
				w.WriteHeader(code)
			}))
			defer receiver.Close()
			s.testEndpoint(t, receiver.URL)
			publish(s, "key", body)
			// Model an abandoned reservation after process death.
			s.DB.Exec("UPDATE deliveries SET status='PROCESSING',token='abandoned',lease=0")
			count := 1
			if code == 503 {
				count = 5
			}
			for i := 0; i < count; i++ {
				s.DB.Exec("UPDATE deliveries SET next=0")
				if err := s.step(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			var status string
			var attempts int
			s.DB.QueryRow("SELECT status,attempts FROM deliveries").Scan(&status, &attempts)
			if status != "DEAD" || attempts != count || int(calls.Load()) != count {
				t.Fatal(status, attempts, calls.Load())
			}
			var n int
			s.DB.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
			if n != count {
				t.Fatal("history lost")
			}
		})
	}
}
func TestExclusiveReservation(t *testing.T) {
	s, _ := setup(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer receiver.Close()
	s.testEndpoint(t, receiver.URL)
	publish(s, "key", body)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.step(context.Background()); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestExpiredTokenCannotFinalize(t *testing.T) {
	s, _ := setup(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.DB.Exec("UPDATE deliveries SET lease=0,token='replacement'"); e != nil {
			t.Error(e)
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	s.testEndpoint(t, receiver.URL)
	publish(s, "key", body)
	if err := s.step(context.Background()); err == nil {
		t.Fatal("expired lease accepted")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
	if n != 0 {
		t.Fatal("stale worker wrote history")
	}
}
func TestLimitsAndTransportFailure(t *testing.T) {
	s, _ := setup(t)
	if publish(s, "large", string(bytes.Repeat([]byte("x"), MaxBody+1))).Code != 413 {
		t.Fatal("missing body limit")
	}
	s.testEndpoint(t, "http://127.0.0.1:1")
	publish(s, "key", body)
	if e := s.step(context.Background()); e != nil {
		t.Fatal(e)
	}
	var status string
	s.DB.QueryRow("SELECT status FROM deliveries").Scan(&status)
	if status != "RETRYING" {
		t.Fatal(status)
	}
}

const testToken = "test-token-123456789"
const testSecret = "test-secret-123456789"

func (s *Service) testEndpoint(t *testing.T, url string) {
	t.Helper()
	if _, err := s.DB.Exec("UPDATE endpoints SET url=? WHERE id='test'", url); err != nil {
		t.Fatal(err)
	}
	client, err := destination.Client(url, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Endpoints["test"] = endpointRuntime{URL: url, Secret: testSecret, Client: client}
}

func (s *Service) step(ctx context.Context) error { _, err := s.dispatchOne(ctx); return err }
