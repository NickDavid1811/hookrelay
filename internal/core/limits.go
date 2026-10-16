package core

import (
	"errors"
	"time"
)

// Limits defines capacity bounds, quotas, retry ceilings, and retention periods.
type Limits struct {
	// Endpoints and ClientEndpoints bound all retained endpoint records, including inactive history.
	Endpoints       int
	ClientEndpoints int
	// Queue is the global maximum number of pending/retrying deliveries allowed in storage.
	Queue int
	// ClientQueue is the maximum number of pending/retrying deliveries allowed per tenant.
	ClientQueue int
	// Events is the maximum total number of retained events in SQLite.
	Events int
	// Cycles is the maximum number of manual replay cycles allowed per delivery.
	Cycles int
	// MaxAttempts is the maximum delivery attempts allowed per cycle before transitioning to DEAD.
	MaxAttempts int
	// PayloadBytes is the maximum total retained payload size in bytes.
	PayloadBytes int64
	// SuccessRetention is the duration after which SUCCESS deliveries are eligible for cleanup.
	SuccessRetention time.Duration
	// DeadRetention is the duration after which DEAD deliveries are eligible for cleanup.
	DeadRetention time.Duration
	// CleanupInterval is the frequency at which the background retention worker sweeps expired events.
	CleanupInterval time.Duration
	// CleanupBatch is the maximum number of expired event graphs deleted per cleanup transaction.
	CleanupBatch int
}

// DefaultLimits returns the recommended production default limits.
func DefaultLimits() Limits {
	return Limits{
		Endpoints:        1000,
		ClientEndpoints:  100,
		Queue:            10000,
		ClientQueue:      1000,
		Events:           20000,
		Cycles:           10,
		MaxAttempts:      5,
		PayloadBytes:     256 << 20, // 256 MiB
		SuccessRetention: 7 * 24 * time.Hour,
		DeadRetention:    30 * 24 * time.Hour,
		CleanupInterval:  time.Minute,
		CleanupBatch:     100,
	}
}

// Validate verifies that configured limits are positive and fall within supported operational bounds.
func (l Limits) Validate() error {
	if l.Endpoints < 1 || l.ClientEndpoints < 1 {
		return errors.New("endpoint limits must be positive")
	}
	if l.Queue < 1 || l.ClientQueue < 1 || l.Events < 1 || l.PayloadBytes < 1 {
		return errors.New("limits must be positive; cycles/batch at most 1000; retention/interval at least 1s")
	}
	if l.Cycles < 1 || l.Cycles > 1000 {
		return errors.New("limits must be positive; cycles/batch at most 1000; retention/interval at least 1s")
	}
	if l.MaxAttempts < 1 || l.MaxAttempts > 50 {
		return errors.New("max attempts must be between 1 and 50")
	}
	if l.CleanupBatch < 1 || l.CleanupBatch > 1000 {
		return errors.New("limits must be positive; cycles/batch at most 1000; retention/interval at least 1s")
	}
	if l.SuccessRetention < time.Second || l.DeadRetention < time.Second || l.CleanupInterval < time.Second {
		return errors.New("limits must be positive; cycles/batch at most 1000; retention/interval at least 1s")
	}
	return nil
}
