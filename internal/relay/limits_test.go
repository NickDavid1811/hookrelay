package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentAdmissionAndIdempotency(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.Queue = 3
	s.Limits.ClientQueue = 3
	var accepted, rejected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := publish(s, ID(), body)
			switch w.Code {
			case 202:
				accepted.Add(1)
			case 429:
				rejected.Add(1)
			default:
				t.Errorf("unexpected %d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 3 || rejected.Load() != 17 {
		t.Fatal(accepted.Load(), rejected.Load())
	}
	for _, table := range []string{"events", "deliveries", "keys"} {
		var n int
		s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
		if n != 3 {
			t.Fatal("partial acceptance", table, n)
		}
	}
	// A retry of accepted publication must succeed despite full capacity.
	var key string
	s.DB.QueryRow("SELECT key FROM keys LIMIT 1").Scan(&key)
	if w := publish(s, key, body); w.Code != 202 {
		t.Fatal("idempotency blocked by capacity", w.Code)
	}
	if w := publish(s, key, body+" "); w.Code != 409 {
		t.Fatal("idempotency conflict lost")
	}
}
func TestRetainedCapacityAndReplay(t *testing.T) {
	for _, kind := range []string{"global queue", "events", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := setup(t)
			s.Limits = DefaultLimits()
			switch kind {
			case "global queue":
				s.Limits.Queue = 1
			case "events":
				s.Limits.Events = 1
			case "bytes":
				s.Limits.PayloadBytes = int64(len(`{"invoice_id":"inv_1"}`))
			}
			w := publish(s, "one", body)
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body)
			}
			if kind != "global queue" {
				s.DB.Exec("UPDATE deliveries SET status='SUCCESS',terminal_at=?", time.Now().UnixMilli())
			}
			if w = publish(s, "two", body); w.Code != 503 {
				t.Fatal("capacity not enforced", w.Code)
			}
		})
	}
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.Queue = 1
	w := publish(s, "dead", body)
	var dead Result
	json.Unmarshal(w.Body.Bytes(), &dead)
	s.DB.Exec("UPDATE deliveries SET status='DEAD',terminal_at=?", time.Now().UnixMilli())
	if publish(s, "pending", body).Code != 202 {
		t.Fatal("terminal delivery occupied queue")
	}
	if replayRequest(s, dead.DeliveryID, 1).Code != 503 {
		t.Fatal("replay bypassed queue")
	}
	var cycle int
	s.DB.QueryRow("SELECT cycle FROM deliveries WHERE id=?", dead.DeliveryID).Scan(&cycle)
	if cycle != 1 {
		t.Fatal("rejected replay mutated")
	}
	var audit int
	s.DB.QueryRow("SELECT count(*) FROM replays").Scan(&audit)
	if audit != 0 {
		t.Fatal("rejected replay audited as accepted")
	}
	s.DB.Exec("UPDATE deliveries SET status='SUCCESS' WHERE id!=?", dead.DeliveryID)
	s.Limits.Cycles = 1
	if replayRequest(s, dead.DeliveryID, 1).Code != 409 {
		t.Fatal("unbounded replay cycles")
	}
}
func TestClientQueueIsolation(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.ClientQueue = 1
	cfg := Config{Clients: []ClientConfig{{ID: "A", TokenEnv: "A", Endpoints: []EndpointConfig{{ID: "a", URL: "http://127.0.0.1:1", SecretEnv: "S", AllowLoopback: true}}}, {ID: "B", TokenEnv: "B", Endpoints: []EndpointConfig{{ID: "b", URL: "http://127.0.0.1:1", SecretEnv: "S", AllowLoopback: true}}}}}
	env := func(key string) string { return key + "-12345678901234567890" }
	if err := s.ApplyConfig(cfg, env); err != nil {
		t.Fatal(err)
	}
	a := strings.Replace(body, "test", "a", 1)
	b := strings.Replace(body, "test", "b", 1)
	if call(s, env("A"), "POST", "/api/v1/publish", a).Code != 202 {
		t.Fatal("first client denied")
	}
	// call uses same key; use different bytes + key for the second publication.
	r := httptest.NewRequest("POST", "/api/v1/publish", strings.NewReader(a))
	r.Header.Set("Authorization", "Bearer "+env("A"))
	r.Header.Set("Idempotency-Key", "second")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
	if call(s, env("B"), "POST", "/api/v1/publish", b).Code != 202 {
		t.Fatal("one client blocked another below global cap")
	}
}
func TestRetentionGraphProtectionAndRollback(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.SuccessRetention = time.Hour
	s.Limits.DeadRetention = 2 * time.Hour
	s.Limits.CleanupBatch = 1
	now := time.Now()
	old := now.Add(-3 * time.Hour).UnixMilli()
	ids := map[string]Result{}
	for _, state := range []string{"SUCCESS", "DEAD", "PENDING", "RETRYING", "PROCESSING", "recent"} {
		w := publish(s, state, body)
		var v Result
		json.Unmarshal(w.Body.Bytes(), &v)
		ids[state] = v
		status := state
		stamp := old
		if state == "recent" {
			status = "SUCCESS"
			stamp = now.UnixMilli()
		}
		s.DB.Exec("UPDATE deliveries SET status=?,terminal_at=? WHERE id=?", status, stamp, v.DeliveryID)
		s.DB.Exec("INSERT INTO attempts(delivery_id,number,http_status,error,completed,cycle) VALUES(?,1,503,'',?,1)", v.DeliveryID, old)
		if state == "DEAD" {
			s.DB.Exec("INSERT INTO replays VALUES(?,2,'local-client','history',?)", v.DeliveryID, old)
		}
	}
	// First deletion fails: the whole event graph must remain.
	s.DB.Exec(`CREATE TRIGGER reject_cleanup BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT,'test'); END`)
	if _, err := s.Cleanup(context.Background(), now); err == nil {
		t.Fatal("failure not propagated")
	}
	for _, table := range []string{"events", "deliveries", "keys", "attempts"} {
		var n int
		s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
		if n != 6 {
			t.Fatal("partial cleanup", table, n)
		}
	}
	s.DB.Exec("DROP TRIGGER reject_cleanup")
	for i := 0; i < 2; i++ {
		n, err := s.Cleanup(context.Background(), now)
		if err != nil || n != 1 {
			t.Fatal("batch", n, err)
		}
	}
	n, err := s.Cleanup(context.Background(), now)
	if err != nil || n != 0 {
		t.Fatal("deleted protected work", n, err)
	}
	for _, state := range []string{"PENDING", "RETRYING", "PROCESSING", "recent"} {
		var n int
		s.DB.QueryRow("SELECT count(*) FROM deliveries WHERE id=?", ids[state].DeliveryID).Scan(&n)
		if n != 1 {
			t.Fatal("protected delivery deleted", state)
		}
	}
	var refs int
	s.DB.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&refs)
	if refs != 0 {
		t.Fatal("orphan references")
	}
	s.DB.QueryRow("SELECT count(*) FROM replays").Scan(&refs)
	if refs != 0 {
		t.Fatal("old audit retained")
	}
	if w := publish(s, "SUCCESS", body); w.Code != 202 {
		t.Fatal("expired key not reusable")
	}
	var nkeys int
	s.DB.QueryRow("SELECT count(*) FROM keys WHERE key='SUCCESS'").Scan(&nkeys)
	if nkeys != 1 {
		t.Fatal("new key missing")
	}
}
func TestTerminalTimestampAndReplayReset(t *testing.T) {
	s, _ := setup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer server.Close()
	s.testEndpoint(t, server.URL)
	w := publish(s, "key", body)
	var v Result
	json.Unmarshal(w.Body.Bytes(), &v)
	start := time.Now().UnixMilli()
	if err := s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var stamp int64
	s.DB.QueryRow("SELECT terminal_at FROM deliveries").Scan(&stamp)
	if stamp < start {
		t.Fatal("missing terminal timestamp")
	}
	if replayRequest(s, v.DeliveryID, 1).Code != 202 {
		t.Fatal("replay rejected")
	}
	s.DB.QueryRow("SELECT terminal_at FROM deliveries").Scan(&stamp)
	if stamp != 0 {
		t.Fatal("replay retained old expiry")
	}
}

func TestDeadWindowAndMultiDeliveryProtection(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.SuccessRetention = time.Hour
	s.Limits.DeadRetention = 2 * time.Hour
	now := time.Now()
	w := publish(s, "dead", body)
	var dead Result
	json.Unmarshal(w.Body.Bytes(), &dead)
	s.DB.Exec("UPDATE deliveries SET status='DEAD',terminal_at=? WHERE id=?", now.Add(-90*time.Minute).UnixMilli(), dead.DeliveryID)
	if n, err := s.Cleanup(context.Background(), now); err != nil || n != 0 {
		t.Fatal("DEAD deleted before its longer window", n, err)
	}
	w = publish(s, "multiple", body)
	var v Result
	json.Unmarshal(w.Body.Bytes(), &v)
	s.DB.Exec("UPDATE deliveries SET status='SUCCESS',terminal_at=? WHERE id=?", now.Add(-3*time.Hour).UnixMilli(), v.DeliveryID)
	if _, err := s.DB.Exec("INSERT INTO deliveries(id,event_id,status,endpoint_id) VALUES('extra',?,'PENDING','test')", v.EventID); err != nil {
		t.Fatal(err)
	}
	s.DB.Exec("UPDATE endpoints SET active=0")
	if n, err := s.Cleanup(context.Background(), now); err != nil || n != 0 {
		t.Fatal("event with paused unfinished delivery deleted", n, err)
	}
	s.DB.Exec("UPDATE deliveries SET status='SUCCESS',terminal_at=? WHERE id='extra'", now.Add(-3*time.Hour).UnixMilli())
	if n, err := s.Cleanup(context.Background(), now); err != nil || n != 1 {
		t.Fatal("completed graph not removed", n, err)
	}
}
func TestReplayRacingCleanup(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.DeadRetention = time.Second
	now := time.Now()
	w := publish(s, "key", body)
	var v Result
	json.Unmarshal(w.Body.Bytes(), &v)
	s.DB.Exec("UPDATE deliveries SET status='DEAD',terminal_at=?", now.Add(-time.Hour).UnixMilli())
	var wg sync.WaitGroup
	var replayCode int
	var cleanupErr error
	wg.Add(2)
	go func() { defer wg.Done(); replayCode = replayRequest(s, v.DeliveryID, 1).Code }()
	go func() { defer wg.Done(); _, cleanupErr = s.Cleanup(context.Background(), now) }()
	wg.Wait()
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	var rows int
	s.DB.QueryRow("SELECT count(*) FROM deliveries WHERE id=?", v.DeliveryID).Scan(&rows)
	if (replayCode == 202 && rows != 1) || (replayCode == 404 && rows != 0) || (replayCode != 202 && replayCode != 404) {
		t.Fatal("replay/cleanup inconsistent", replayCode, rows)
	}
}
