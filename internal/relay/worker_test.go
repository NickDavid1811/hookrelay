package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func waitWorker(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
	}
}
func TestWorkEligibilitySignal(t *testing.T) {
	s, _ := setup(t)
	if processed, err := s.dispatchOne(context.Background()); processed || err != nil {
		t.Fatal("empty queue reported work", processed, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	s.testEndpoint(t, server.URL)
	publish(s, "key", body)
	s.DB.Exec("UPDATE deliveries SET next=?", time.Now().Add(time.Hour).UnixMilli())
	if processed, err := s.dispatchOne(context.Background()); processed || err != nil {
		t.Fatal("delayed queue reported work", processed, err)
	}
	s.DB.Exec("UPDATE deliveries SET next=0")
	if processed, err := s.dispatchOne(context.Background()); !processed || err != nil {
		t.Fatal("eligible queue not processed", processed, err)
	}
}
func TestConsecutiveWorkAndCancelDuringSend(t *testing.T) {
	s, _ := setup(t)
	var calls, active, maxActive atomic.Int32
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n := active.Add(1)
		defer active.Add(-1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	}))
	defer func() { close(release); server.Close() }()
	s.testEndpoint(t, server.URL)
	for i := 0; i < 3; i++ {
		if publish(s, ID(), body).Code != 202 {
			t.Fatal("publish")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no send")
	}
	// No concurrent outgoing send while the first response is blocked.
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 1 || maxActive.Load() != 1 {
		t.Fatal("more than one active send")
	}
	cancel()
	waitWorker(t, done)
	var attempts int
	s.DB.QueryRow("SELECT count(*) FROM attempts").Scan(&attempts)
	if attempts != 0 {
		t.Fatal("canceled send finalized")
	}
	var processing int
	s.DB.QueryRow("SELECT count(*) FROM deliveries WHERE status='PROCESSING'").Scan(&processing)
	if processing != 1 {
		t.Fatal("canceled send lost recoverable lease")
	}
	if metricValue(t, s, "hookrelay_worker_running", nil) != 0 {
		t.Fatal("running gauge stale")
	}
}
func TestIdleCancelAndDelayedWork(t *testing.T) {
	s, _ := setup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("future work sent early"); w.WriteHeader(204) }))
	defer server.Close()
	s.testEndpoint(t, server.URL)
	publish(s, "future", body)
	s.DB.Exec("UPDATE deliveries SET next=?", time.Now().Add(time.Hour).UnixMilli())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	waitWorker(t, done)
	var status string
	s.DB.QueryRow("SELECT status FROM deliveries").Scan(&status)
	if status != "PENDING" {
		t.Fatal(status)
	}
	// Pre-canceled context must never reserve anything.
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestCleanupNotStarvedByBusyQueue(t *testing.T) {
	s, _ := setup(t)
	s.Limits = DefaultLimits()
	s.Limits.CleanupInterval = time.Second
	s.Limits.SuccessRetention = time.Second
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(20 * time.Millisecond); w.WriteHeader(204) }))
	defer server.Close()
	s.testEndpoint(t, server.URL)
	publish(s, "old", body)
	s.DB.Exec("UPDATE deliveries SET status='SUCCESS',terminal_at=1")
	for i := 0; i < 70; i++ {
		publish(s, ID(), body)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	deleted := false
	for time.Now().Before(deadline) {
		var old int
		s.DB.QueryRow("SELECT count(*) FROM keys WHERE key='old'").Scan(&old)
		if old == 0 {
			deleted = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	waitWorker(t, done)
	if !deleted {
		t.Fatal("maintenance starved")
	}
	var remaining int
	s.DB.QueryRow("SELECT count(*) FROM deliveries WHERE status IN ('PENDING','PROCESSING','RETRYING')").Scan(&remaining)
	if remaining == 0 {
		t.Fatal("test did not retain busy queue during maintenance")
	}
}

func TestConcurrentWorkerPool(t *testing.T) {
	s, _ := setup(t)
	s.Workers = 3
	var active, maxActive atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n := active.Add(1)
		defer active.Add(-1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		select {
		case <-release:
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	}))
	defer func() { close(release); server.Close() }()
	s.testEndpoint(t, server.URL)
	for i := 0; i < 6; i++ {
		if publish(s, ID(), body).Code != 202 {
			t.Fatal("publish")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if maxActive.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if maxActive.Load() < 2 {
		t.Fatalf("expected concurrent deliveries with pool, got max %d", maxActive.Load())
	}
	cancel()
	waitWorker(t, done)
}
