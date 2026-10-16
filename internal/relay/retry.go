package relay

import (
	"hookrelay/internal/core"
	"time"
)

func retryDelay(code int, header string, now time.Time, backoff, cap time.Duration) time.Duration {
	return core.RetryDelay(code, header, now, backoff, cap)
}
func retryAfterDelay(header string, now time.Time, cap time.Duration) (time.Duration, bool) {
	return core.RetryAfterDelay(header, now, cap)
}
