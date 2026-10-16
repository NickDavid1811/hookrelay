package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryAfterPolicy(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name, header string
		code         int
		want         time.Duration
	}{
		{"seconds", "30", 429, 30 * time.Second}, {"503", "30", 503, 30 * time.Second},
		{"date", now.Add(time.Minute).Format(http.TimeFormat), 503, time.Minute},
		{"expired", now.Add(-time.Minute).Format(http.TimeFormat), 429, 2 * time.Second},
		{"invalid", "soon", 429, 2 * time.Second}, {"negative", "-1", 429, 2 * time.Second},
		{"fraction", "1.5", 429, 2 * time.Second}, {"plus", "+3", 429, 2 * time.Second},
		{"missing", "", 429, 2 * time.Second}, {"zero", "0", 429, 2 * time.Second},
		{"minimum", "1", 429, 2 * time.Second}, {"spaces", " \t30\t ", 429, 30 * time.Second},
		{"cap seconds", "99999", 429, time.Minute}, {"overflow", "999999999999999999999999999999", 429, time.Minute},
		{"cap date", now.Add(time.Hour).Format(http.TimeFormat), 503, time.Minute},
		{"other status", "30", 500, 2 * time.Second}, {"duplicate joined", "30, 40", 429, 2 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := retryDelay(tc.code, tc.header, now, 2*time.Second, time.Minute)
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	if got := retryDelay(429, "999999", now, time.Second, 0); got != DefaultRetryAfterMax {
		t.Fatal(got)
	}
}

func TestRetryAfterPersistsAcrossReopen(t *testing.T) {
	s, path := setup(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer receiver.Close()
	s.testEndpoint(t, receiver.URL)
	s.RetryAfterMax = 2 * time.Second
	publish(s, "key", body)
	start := time.Now()
	if err := s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var next int64
	s.DB.QueryRow("SELECT next FROM deliveries").Scan(&next)
	if next < start.Add(2*time.Second).UnixMilli() || next > time.Now().Add(2*time.Second).UnixMilli() {
		t.Fatal("unbounded or shortened deadline", next)
	}
	s.DB.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB = db
	defer db.Close()
	if err = s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("early delivery after reopen")
	}
	remaining := time.Until(time.UnixMilli(next))
	if remaining > 0 {
		time.Sleep(remaining + 10*time.Millisecond)
	}
	if err = s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	s.DB.QueryRow("SELECT status,attempts FROM deliveries").Scan(&status, &attempts)
	if status != "SUCCESS" || attempts != 2 {
		t.Fatal(status, attempts)
	}
}
