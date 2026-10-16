package core

import (
	"strconv"
	"strings"
	"time"
)

const DefaultRetryAfterMax = 5 * time.Minute

// A valid Retry-After is a bounded minimum, never shortened by jitter.
// Very large digit-only values saturate instead of overflowing a duration.
func RetryAfterDelay(value string, now time.Time, cap time.Duration) (time.Duration, bool) {
	value = strings.Trim(value, " \t")
	if value == "" {
		return 0, false
	}
	digits := true
	for _, c := range value {
		if c < '0' || c > '9' {
			digits = false
			break
		}
	}
	if digits {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(cap/time.Second) {
			return cap, true
		}
		wait := time.Duration(seconds) * time.Second
		if wait > cap {
			wait = cap
		}
		return wait, true
	}
	date, err := parseHTTPTime(value)
	if err != nil || !date.After(now) {
		return 0, false
	}
	wait := date.Sub(now)
	if wait > cap {
		wait = cap
	}
	return wait, true
}

func RetryDelay(status int, header string, now time.Time, backoff, cap time.Duration) time.Duration {
	if cap <= 0 {
		cap = DefaultRetryAfterMax
	}
	if status == 429 || status == 503 {
		if wait, ok := RetryAfterDelay(header, now, cap); ok && wait > backoff {
			return wait
		}
	}
	return backoff
}

func parseHTTPTime(value string) (time.Time, error) {
	var parsed time.Time
	var err error
	for _, layout := range []string{"Mon, 02 Jan 2006 15:04:05 GMT", time.RFC850, time.ANSIC} {
		parsed, err = time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return parsed, err
}

// MaxBackoff bounds exponential growth independently of Retry-After's separate ceiling.
const MaxBackoff = 5 * time.Minute

// BackoffCeiling preserves initial 1/2/4/8 second bases and saturates before multiplying.
func BackoffCeiling(attempts int) time.Duration {
	delay := time.Second
	for i := 0; i < attempts; i++ {
		if delay >= MaxBackoff/2 {
			return MaxBackoff
		}
		delay *= 2
	}
	return delay
}
