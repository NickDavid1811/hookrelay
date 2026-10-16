package core

import (
	"net/http"
	"testing"
	"time"
)

// Moving Retry-After parsing into the core preserves the standard HTTP date formats.
func TestHTTPDateParity(t *testing.T) {
	for _, value := range []string{
		"Sun, 06 Nov 1994 08:49:37 GMT",
		"Sunday, 06-Nov-94 08:49:37 GMT",
		"Sun Nov  6 08:49:37 1994",
		"bad date", "Sun, 99 Nov 1994 08:49:37 GMT",
	} {
		want, e1 := http.ParseTime(value)
		got, e2 := parseHTTPTime(value)
		if (e1 == nil) != (e2 == nil) || e1 == nil && !got.Equal(want) {
			t.Fatalf("%q: got %v/%v want %v/%v", value, got, e2, want, e1)
		}
	}
}
func TestDeliveryPolicy(t *testing.T) {
	for _, tc := range []struct {
		code, attempt int
		reason, want  string
	}{
		{204, 0, "", "SUCCESS"}, {204, 0, "response read failure", "DEAD"},
		{302, 0, "", "DEAD"}, {400, 0, "", "DEAD"}, {408, 0, "", "RETRYING"},
		{429, 0, "", "RETRYING"}, {503, 3, "", "RETRYING"}, {503, 4, "", "DEAD"},
		{0, 0, "transport failure", "RETRYING"},
	} {
		if got := DeliveryStatus(tc.code, tc.reason, tc.attempt, 5); got != tc.want {
			t.Errorf("%+v: %s", tc, got)
		}
	}
	if got := DeliveryStatus(503, "", 2, 3); got != "DEAD" {
		t.Errorf("custom limit of 3 should transition to DEAD at attempt 2, got %s", got)
	}
	now := time.Now()
	if got := RetryDelay(503, "9999999999999999999999999", now, time.Second, time.Minute); got != time.Minute {
		t.Fatal(got)
	}
}

func TestBackoffSaturatesBeforeOverflow(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{{0, time.Second}, {1, 2 * time.Second}, {3, 8 * time.Second}, {8, 256 * time.Second}, {9, MaxBackoff}, {34, MaxBackoff}, {49, MaxBackoff}, {100000, MaxBackoff}} {
		if got := BackoffCeiling(tc.attempt); got != tc.want {
			t.Errorf("attempt %d: got %s want %s", tc.attempt, got, tc.want)
		}
	}
}
