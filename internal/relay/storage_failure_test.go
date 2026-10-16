package relay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hookrelay/internal/core"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func storageSetup(t *testing.T) (*Service, string) {
	t.Helper()
	s, path := setup(t)
	reader, err := OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	s.ReadDB = reader
	t.Cleanup(func() { reader.Close() })
	return s, path
}

func requireSQLiteFailure(t *testing.T, err error, codes ...int) {
	t.Helper()
	var failure *sqlite.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected SQLite failure, got %v", err)
	}
	for _, code := range codes {
		if failure.Code()&255 == code {
			t.Logf("SQLite failure confirmed: code=%d", failure.Code())
			return
		}
	}
	t.Fatalf("unexpected SQLite code %d: %v", failure.Code(), err)
}

// Fail at a specific statement using a real SQLITE_FULL, after earlier writes
// in the transaction have run. This is a database page ceiling, not host ENOSPC.
func exhaustSQLiteAt(t *testing.T, s *Service, trigger string) func() {
	t.Helper()
	mustExec(t, s.DB, "CREATE TABLE storage_pressure(body BLOB)")
	var original, pages int64
	if err := s.DB.QueryRow("PRAGMA max_page_count").Scan(&original); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s.DB, fmt.Sprintf("PRAGMA max_page_count=%d", pages))
	mustExec(t, s.DB, "CREATE TEMP TRIGGER storage_fault "+trigger+" BEGIN INSERT INTO storage_pressure VALUES(zeroblob(1048576)); END")
	var once sync.Once
	release := func() {
		once.Do(func() {
			mustExec(t, s.DB, "DROP TRIGGER storage_fault")
			mustExec(t, s.DB, fmt.Sprintf("PRAGMA max_page_count=%d", original))
			mustExec(t, s.DB, "DROP TABLE storage_pressure")
		})
	}
	t.Cleanup(release)
	return release
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func storedGraph(t *testing.T, s *Service) map[string][][]any {
	t.Helper()
	out := map[string][][]any{}
	for _, table := range []string{"events", "deliveries", "keys", "attempts", "replays", "clients", "endpoints"} {
		rows, err := s.DB.Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err = rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			out[table] = append(out[table], values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func requireGraph(t *testing.T, s *Service, want map[string][][]any) {
	t.Helper()
	actual := storedGraph(t, s)
	for table, rows := range want {
		if !reflect.DeepEqual(rows, actual[table]) {
			t.Fatalf("partial transaction changed %s", table)
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("transaction changed the stored graph")
	}
}
func requireIntegrity(t *testing.T, s *Service) {
	t.Helper()
	var result string
	if err := s.DB.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Fatalf("integrity: %q %v", result, err)
	}
	var violations int
	if err := s.DB.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("references: %d %v", violations, err)
	}
}
func accepted(t *testing.T, s *Service, key string) core.Result {
	t.Helper()
	w := publish(s, key, body)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var result core.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func storageRequest(s *Service, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func requireUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 503 || w.Body.String() != "{\"error\":\"storage unavailable\"}\n" {
		t.Fatalf("expected sanitized storage 503: %d %s", w.Code, w.Body)
	}
}
func storageReceiver(s *Service, code int, received *[]string, beforeAck func()) {
	ep := s.Endpoints["test"]
	ep.Client = &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		io.Copy(io.Discard, r.Body)
		*received = append(*received, r.Header.Get("X-Webhook-Delivery-Id"))
		if beforeAck != nil {
			beforeAck()
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	s.Endpoints["test"] = ep
}
func requireDelivery(t *testing.T, s *Service, id, status string, attempts int) {
	t.Helper()
	r, err := s.application().Delivery(context.Background(), "local-client", id)
	if err != nil || r.Status != status || r.Attempts != attempts {
		t.Fatalf("delivery status=%s attempts=%d err=%v, want %s/%d", r.Status, r.Attempts, err, status, attempts)
	}
}

func TestStorageFullPublicationIsAtomicAndRetryable(t *testing.T) {
	s, path := storageSetup(t)
	release := exhaustSQLiteAt(t, s, "BEFORE INSERT ON keys")
	before := storedGraph(t, s)
	_, err := s.application().Publish(context.Background(), "local-client", "full", []byte(body))
	requireSQLiteFailure(t, err, 13)
	requireUnavailable(t, publish(s, "full", body))
	requireGraph(t, s, before)
	requireIntegrity(t, s)
	if metricValue(t, s, "hookrelay_publications_total", map[string]string{"result": "new"}) != 0 {
		t.Fatal("failed publication counted as accepted")
	}
	release()
	receipt := accepted(t, s, "full")
	repeated := accepted(t, s, "full")
	if receipt.EventID != repeated.EventID || receipt.DeliveryID != repeated.DeliveryID {
		t.Fatal("retry lost idempotency")
	}
	reopenStorage(t, s, path)
	requireDelivery(t, s, receipt.DeliveryID, "PENDING", 0)
	requireIntegrity(t, s)
}
func TestStorageFullEndpointCreationRollsBack(t *testing.T) {
	s, _ := storageSetup(t)
	release := exhaustSQLiteAt(t, s, "AFTER INSERT ON endpoints")
	before := storedGraph(t, s)
	request := `{"id":"disk-failure","url":"https://example.com/hook"}`
	requireUnavailable(t, storageRequest(s, "POST", "/api/v1/endpoints", request))
	requireGraph(t, s, before)
	release()
	w := storageRequest(s, "POST", "/api/v1/endpoints", request)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	requireIntegrity(t, s)
}
func TestStorageFullClaimDoesNotSend(t *testing.T) {
	s, _ := storageSetup(t)
	receipt := accepted(t, s, "claim")
	received := []string{}
	storageReceiver(s, 204, &received, nil)
	release := exhaustSQLiteAt(t, s, "AFTER UPDATE OF status ON deliveries WHEN NEW.status='PROCESSING'")
	before := storedGraph(t, s)
	_, err := s.dispatchOne(context.Background())
	requireSQLiteFailure(t, err, 13)
	requireGraph(t, s, before)
	if len(received) != 0 {
		t.Fatal("sent an uncommitted claim")
	}
	release()
	if _, err = s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireDelivery(t, s, receipt.DeliveryID, "SUCCESS", 1)
	requireIntegrity(t, s)
}
func TestStorageFullReplayRollsBackAuditAndCycle(t *testing.T) {
	s, _ := storageSetup(t)
	received := []string{}
	storageReceiver(s, 400, &received, nil)
	receipt := accepted(t, s, "replay")
	if _, err := s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireDelivery(t, s, receipt.DeliveryID, "DEAD", 1)
	release := exhaustSQLiteAt(t, s, "BEFORE INSERT ON replays")
	before := storedGraph(t, s)
	path := "/api/v1/deliveries/" + receipt.DeliveryID + "/replay"
	requireUnavailable(t, storageRequest(s, "POST", path, `{"expected_cycle":1,"reason":"retry"}`))
	requireGraph(t, s, before)
	release()
	w := storageRequest(s, "POST", path, `{"expected_cycle":1,"reason":"retry"}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	result, err := s.application().Delivery(context.Background(), "local-client", receipt.DeliveryID)
	if err != nil || result.Cycle != 2 || result.Status != "PENDING" {
		t.Fatal(result, err)
	}
	replays, err := s.application().Replays(context.Background(), "local-client", receipt.DeliveryID, 0)
	if err != nil || len(replays) != 1 {
		t.Fatal("replay audit not atomic", len(replays), err)
	}
	requireIntegrity(t, s)
}
func TestStorageFullCleanupPreservesWholeHistory(t *testing.T) {
	s, _ := storageSetup(t)
	received := []string{}
	storageReceiver(s, 204, &received, nil)
	accepted(t, s, "cleanup")
	if _, err := s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	release := exhaustSQLiteAt(t, s, "BEFORE DELETE ON events")
	before := storedGraph(t, s)
	later := time.Now().Add(8 * 24 * time.Hour)
	n, err := s.Cleanup(context.Background(), later)
	requireSQLiteFailure(t, err, 13)
	if n != 0 {
		t.Fatal("reported uncommitted cleanup", n)
	}
	requireGraph(t, s, before)
	release()
	n, err = s.Cleanup(context.Background(), later)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	for _, table := range []string{"events", "deliveries", "keys", "attempts", "replays"} {
		var n int
		if err = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	requireIntegrity(t, s)
}
func TestStorageReadonlyWriterRejectsMutationAndRecovers(t *testing.T) {
	s, _ := storageSetup(t)
	receipt := accepted(t, s, "already-accepted")
	before := storedGraph(t, s)
	mustExec(t, s.DB, "PRAGMA query_only=ON")
	t.Cleanup(func() { mustExec(t, s.DB, "PRAGMA query_only=OFF") })
	_, err := s.application().Publish(context.Background(), "local-client", "readonly", []byte(body))
	requireSQLiteFailure(t, err, 8)
	requireUnavailable(t, publish(s, "readonly", body))
	received := []string{}
	storageReceiver(s, 204, &received, nil)
	_, err = s.dispatchOne(context.Background())
	requireSQLiteFailure(t, err, 8)
	if len(received) != 0 {
		t.Fatal("sent without a committed lease")
	}
	requireGraph(t, s, before)
	mustExec(t, s.DB, "PRAGMA query_only=OFF")
	accepted(t, s, "readonly")
	if _, err = s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireDelivery(t, s, receipt.DeliveryID, "SUCCESS", 1)
	requireIntegrity(t, s)
}
func reopenStorage(t *testing.T, s *Service, path string) {
	t.Helper()
	if s.ReadDB != nil {
		if err := s.ReadDB.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, reader, err := OpenPool(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB, s.ReadDB = db, reader
	t.Cleanup(func() { reader.Close(); db.Close() })
}

func TestStorageFullFinalizationStopsWorkerAndRecovers(t *testing.T) {
	s, path := storageSetup(t)
	receipt := accepted(t, s, "finalize")
	received := []string{}
	storageReceiver(s, 204, &received, nil)
	release := exhaustSQLiteAt(t, s, "BEFORE INSERT ON attempts")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.Run(ctx)
	requireSQLiteFailure(t, err, 13)
	if len(received) != 1 {
		t.Fatal("receiver should have accepted once", len(received))
	}
	if metricValue(t, s, "hookrelay_worker_running", nil) != 0 {
		t.Fatal("failed worker still reported as running")
	}
	requireDelivery(t, s, receipt.DeliveryID, "PROCESSING", 0)
	if metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}) != 0 {
		t.Fatal("uncommitted success counted")
	}
	var attempts int
	if err = s.DB.QueryRow("SELECT count(*) FROM attempts").Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal(attempts, err)
	}
	release()
	reopenStorage(t, s, path)
	requireDelivery(t, s, receipt.DeliveryID, "PROCESSING", 0)
	// Advance lease expiry in the fixture, without sleeping 30 seconds. The
	// existing SIGKILL process suite independently tests real-time lease expiry.
	mustExec(t, s.DB, "UPDATE deliveries SET lease=0 WHERE id=?", receipt.DeliveryID)
	if _, err = s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0] != receipt.DeliveryID || received[1] != receipt.DeliveryID {
		t.Fatal("recovery did not resend the same delivery")
	}
	requireDelivery(t, s, receipt.DeliveryID, "SUCCESS", 1)
	requireIntegrity(t, s)
}

func TestStorageFullConfigurationPreservesPreviousOwners(t *testing.T) {
	s, _ := storageSetup(t)
	release := exhaustSQLiteAt(t, s, "AFTER INSERT ON endpoints WHEN NEW.id='new-config'")
	before := storedGraph(t, s)
	cfg := Config{Clients: []ClientConfig{{ID: "replacement", TokenEnv: "NEW_TOKEN", Endpoints: []EndpointConfig{{ID: "new-config", URL: "https://example.com/hook", SecretEnv: "NEW_SECRET"}}}}}
	err := s.ApplyConfig(cfg, func(string) string { return "replacement-test-secret" })
	requireSQLiteFailure(t, err, 13)
	requireGraph(t, s, before)
	if len(s.Endpoints) != 1 {
		t.Fatal("failed configuration changed runtime destinations")
	}
	owner, err := s.application().Authenticate(context.Background(), testToken)
	if err != nil || owner != "local-client" {
		t.Fatal("previous client disabled by failed configuration", owner, err)
	}
	release()
	requireIntegrity(t, s)
}

func TestStorageBusyWriterReturns503ThenRecovers(t *testing.T) {
	s, path := storageSetup(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	lock, err := other.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err = lock.Exec("UPDATE clients SET active=active"); err != nil {
		t.Fatal(err)
	}
	before := storedGraph(t, s)
	// A real competing writer must cause rejection without a partial publication.
	// SQLite can reject a read-to-write upgrade immediately despite busy_timeout.
	_, err = s.application().Publish(context.Background(), "local-client", "busy-writer", []byte(body))
	requireSQLiteFailure(t, err, 5)
	requireUnavailable(t, publish(s, "busy-writer", body))
	requireGraph(t, s, before)
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	accepted(t, s, "busy-writer")
	requireIntegrity(t, s)
}
