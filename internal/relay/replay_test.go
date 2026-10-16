package relay

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func replayRequest(s *Service, id string, cycle int) *httptest.ResponseRecorder {
	data := `{"expected_cycle":` + strconv.Itoa(cycle) + `,"reason":"destination recovered"}`
	r := httptest.NewRequest("POST", "/api/v1/deliveries/"+id+"/replay", strings.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestReplayCyclesAuditAndConcurrency(t *testing.T) {
	s, _ := setup(t)
	var code atomic.Int32
	code.Store(503)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(int(code.Load())) }))
	defer receiver.Close()
	s.testEndpoint(t, receiver.URL)
	publication := publish(s, "key", body)
	var out Result
	json.Unmarshal(publication.Body.Bytes(), &out)
	exhaust := func() {
		for i := 0; i < 5; i++ {
			s.DB.Exec("UPDATE deliveries SET next=0")
			if err := s.step(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	exhaust()
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := replayRequest(s, out.DeliveryID, 1)
			if w.Code == 202 {
				accepted.Add(1)
			} else if w.Code != 409 {
				t.Errorf("unexpected %d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal(accepted.Load())
	}
	exhaust()
	if replayRequest(s, out.DeliveryID, 1).Code != 409 {
		t.Fatal("stale cycle accepted")
	}
	if w := replayRequest(s, out.DeliveryID, 2); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	code.Store(204)
	if err := s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var total, cycle, cycleAttempts int
	var status string
	s.DB.QueryRow("SELECT status,attempts,cycle,cycle_attempts FROM deliveries").Scan(&status, &total, &cycle, &cycleAttempts)
	if status != "SUCCESS" || total != 11 || cycle != 3 || cycleAttempts != 1 {
		t.Fatal(status, total, cycle, cycleAttempts)
	}
	if replayRequest(s, out.DeliveryID, 3).Code != 409 {
		t.Fatal("SUCCESS replay accepted")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
	if n != 11 {
		t.Fatal("history lost", n)
	}
	s.DB.QueryRow("SELECT count(*) FROM replays WHERE actor='local-client' AND reason='destination recovered'").Scan(&n)
	if n != 2 {
		t.Fatal("audit lost", n)
	}
	r := httptest.NewRequest("GET", "/api/v1/deliveries/"+out.DeliveryID+"/attempts?after=5", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var history []map[string]any
	json.Unmarshal(w.Body.Bytes(), &history)
	if len(history) != 6 || history[0]["cycle"] != float64(2) {
		t.Fatal(w.Body)
	}
	if w := publish(s, "key", body); w.Code != 202 {
		t.Fatal("publication idempotency lost")
	}
	if replayRequest(s, "missing", 1).Code != 404 {
		t.Fatal("missing delivery")
	}
}
func TestReplayAuditFailureRollsBack(t *testing.T) {
	s, _ := setup(t)
	w := publish(s, "key", body)
	var out Result
	json.Unmarshal(w.Body.Bytes(), &out)
	s.DB.Exec("UPDATE deliveries SET status='DEAD'")
	s.DB.Exec(`CREATE TRIGGER reject_replay BEFORE INSERT ON replays BEGIN SELECT RAISE(ABORT,'test'); END`)
	if replayRequest(s, out.DeliveryID, 1).Code != 503 {
		t.Fatal("false acceptance")
	}
	var status string
	var cycle int
	s.DB.QueryRow("SELECT status,cycle FROM deliveries").Scan(&status, &cycle)
	if status != "DEAD" || cycle != 1 {
		t.Fatal("partial replay", status, cycle)
	}
}
func TestMigrateV1AndRejectFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE events(id TEXT PRIMARY KEY,kind TEXT NOT NULL,body BLOB NOT NULL,created INTEGER NOT NULL);
 CREATE TABLE deliveries(id TEXT PRIMARY KEY,event_id TEXT NOT NULL REFERENCES events(id),status TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,next INTEGER NOT NULL DEFAULT 0,lease INTEGER NOT NULL DEFAULT 0,token TEXT NOT NULL DEFAULT '',http_status INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '');
 CREATE TABLE keys(key TEXT PRIMARY KEY,hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES events(id),delivery_id TEXT NOT NULL REFERENCES deliveries(id));
 CREATE TABLE attempts(delivery_id TEXT NOT NULL REFERENCES deliveries(id),number INTEGER NOT NULL,http_status INTEGER NOT NULL,error TEXT NOT NULL,completed INTEGER NOT NULL,PRIMARY KEY(delivery_id,number));
 INSERT INTO events VALUES('event','test','{}',1); INSERT INTO deliveries(id,event_id,status,attempts) VALUES('delivery','event','DEAD',5);
 INSERT INTO attempts VALUES('delivery',5,503,'',1); PRAGMA user_version=1;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var cycle, attempts int
	db.QueryRow("SELECT cycle,cycle_attempts FROM deliveries").Scan(&cycle, &attempts)
	if cycle != 1 || attempts != 5 {
		t.Fatal(cycle, attempts)
	}
	db.QueryRow("SELECT cycle FROM attempts").Scan(&cycle)
	if cycle != 1 {
		t.Fatal("old history not migrated")
	}
	var clientID, endpointID string
	db.QueryRow("SELECT e.client_id,d.endpoint_id FROM events e JOIN deliveries d ON d.event_id=e.id").Scan(&clientID, &endpointID)
	if clientID != "local-client" || endpointID != "test" {
		t.Fatal("legacy ownership not backfilled", clientID, endpointID)
	}
	var fk int
	db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys not restored")
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal("reopen migration", err)
	}
	db.Exec("PRAGMA user_version=99")
	db.Close()
	if db, err = Open(path); err == nil {
		db.Close()
		t.Fatal("future version overwritten")
	}
}
