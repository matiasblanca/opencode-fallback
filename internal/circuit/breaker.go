package circuit

import (
	"log/slog"
	"sync"
	"time"
)

// State represents the current state of a CircuitBreaker.
type State int

const (
	// StateClosed is the normal operating state — requests pass through.
	StateClosed State = iota
	// StateOpen is the tripped state — all requests are blocked.
	StateOpen
	// StateHalfOpen is the recovery probe state — one request is allowed through.
	StateHalfOpen
)

// String returns a human-readable name for the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config holds circuit breaker parameters.
type Config struct {
	// FailureThreshold is the number of failures within FailureWindow that
	// trigger the Open state.
	FailureThreshold int
	// FailureWindow is the rolling time window in which failures are counted.
	FailureWindow time.Duration
	// OpenDuration is how long the circuit stays Open before transitioning to
	// HalfOpen for a probe request.
	OpenDuration time.Duration
}

// DefaultConfig returns the default circuit breaker configuration matching the
// architecture specification.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 3,
		FailureWindow:    time.Minute,
		OpenDuration:     30 * time.Second,
	}
}

// CircuitBreaker implements the circuit-breaker pattern for a single provider.
// It is safe for concurrent use from multiple goroutines.
//
// State machine:
//
//	Closed ──(threshold failures)──► Open ──(OpenDuration elapsed)──► HalfOpen
//	  ▲                                                                   │
//	  └──────────────────(probe succeeds)──────────────────────────────── ┘
//	                          │
//	           (probe fails)──► Open
type CircuitBreaker struct {
	mu sync.Mutex

	providerID string
	state      State

	failureCount    int
	lastFailureTime time.Time
	openedAt        time.Time

	// FailureThreshold is the number of failures within FailureWindow that open
	// the circuit.
	FailureThreshold int
	// FailureWindow is the rolling window for counting failures.
	FailureWindow time.Duration
	// OpenDuration is how long the circuit stays open before attempting a probe.
	OpenDuration time.Duration

	logger *slog.Logger

	// now is injected in tests to control the clock; defaults to time.Now.
	now func() time.Time
}

// New creates a CircuitBreaker for the given providerID with default
// configuration.
func New(providerID string, logger *slog.Logger) *CircuitBreaker {
	return NewWithConfig(providerID, DefaultConfig(), logger)
}

// NewWithConfig creates a CircuitBreaker for the given providerID with the
// supplied configuration.
func NewWithConfig(providerID string, cfg Config, logger *slog.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		providerID:       providerID,
		state:            StateClosed,
		FailureThreshold: cfg.FailureThreshold,
		FailureWindow:    cfg.FailureWindow,
		OpenDuration:     cfg.OpenDuration,
		logger:           logger,
		now:              time.Now,
	}
}

// Allow reports whether a request to the provider should be allowed.
//
//   - Closed: always returns true.
//   - Open: returns false, unless OpenDuration has elapsed — in which case it
//     transitions to HalfOpen and returns true for the single probe request.
//   - HalfOpen: returns false (the probe has already been dispatched).
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		if cb.now().Sub(cb.openedAt) >= cb.OpenDuration {
			cb.transitionTo(StateHalfOpen)
			return true // single probe
		}
		return false

	case StateHalfOpen:
		// The probe slot is already in-flight; block additional requests.
		return false
	}

	return false
}

// RecordSuccess records a successful request outcome.
//
//   - HalfOpen: transitions back to Closed and resets all counters.
//   - Closed: resets failure counters (idempotent, safe to call always).
//   - Open: no-op (success cannot be recorded when requests are blocked).
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateHalfOpen:
		cb.transitionTo(StateClosed)
		cb.resetCounters()
	case StateClosed:
		cb.resetCounters()
	}
	// StateOpen: ignore — requests are blocked so no valid success can occur.
}

// RecordFailure records a failed request outcome.
//
//   - HalfOpen: the probe failed; transitions back to Open immediately.
//   - Closed: increments the failure counter. If the counter reaches
//     FailureThreshold within FailureWindow the circuit opens.
//   - Open: no-op.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.now()

	switch cb.state {
	case StateHalfOpen:
		// Probe failed — reopen.
		cb.openedAt = now
		cb.transitionTo(StateOpen)
		return

	case StateClosed:
		// If the last failure was recorded outside the window, reset the counter
		// so stale failures do not contribute to the threshold.
		if cb.failureCount > 0 && now.Sub(cb.lastFailureTime) > cb.FailureWindow {
			cb.resetCounters()
		}

		cb.failureCount++
		cb.lastFailureTime = now

		if cb.failureCount >= cb.FailureThreshold {
			cb.openedAt = now
			cb.transitionTo(StateOpen)
		}
	}
	// StateOpen: ignore.
}

// CurrentState returns the current state of the circuit breaker.
// It is safe to call from multiple goroutines.
func (cb *CircuitBreaker) CurrentState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Reset unconditionally returns the circuit breaker to the Closed state and
// clears all counters. Primarily useful in tests and administrative tooling.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(StateClosed)
	cb.resetCounters()
}

// --------------------------------------------------------------------------
// Internal helpers (called with mu held)
// --------------------------------------------------------------------------

// transitionTo changes the state and emits a structured log entry.
func (cb *CircuitBreaker) transitionTo(next State) {
	prev := cb.state
	cb.state = next
	cb.logger.Info("circuit breaker state transition",
		"provider", cb.providerID,
		"from", prev.String(),
		"to", next.String(),
	)
}

// resetCounters zeroes failure tracking fields.
func (cb *CircuitBreaker) resetCounters() {
	cb.failureCount = 0
	cb.lastFailureTime = time.Time{}
	cb.openedAt = time.Time{}
}
