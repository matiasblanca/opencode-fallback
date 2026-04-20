package circuit

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// testLogger returns a slog.Logger suitable for tests (discards output).
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fixedNow returns a func() time.Time that always returns t.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// --------------------------------------------------------------------------
// State: initial state
// --------------------------------------------------------------------------

func TestNewCircuitBreakerStartsClosed(t *testing.T) {
	cb := New("test-provider", testLogger())
	if cb.CurrentState() != StateClosed {
		t.Errorf("new circuit breaker state = %v, want %v", cb.CurrentState(), StateClosed)
	}
}

// --------------------------------------------------------------------------
// Allow — state-based gate
// --------------------------------------------------------------------------

func TestAllowWhenClosed(t *testing.T) {
	cb := New("test-provider", testLogger())
	if !cb.Allow() {
		t.Error("Allow() = false when Closed, want true")
	}
}

func TestAllowWhenOpen(t *testing.T) {
	cb := New("test-provider", testLogger())
	now := time.Now()
	cb.now = fixedNow(now)

	// Force open state by exceeding threshold.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}

	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected state Open after %d failures, got %v", cb.FailureThreshold, cb.CurrentState())
	}
	if cb.Allow() {
		t.Error("Allow() = true when Open, want false")
	}
}

func TestAllowWhenOpenTransitionsToHalfOpen(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Drive to Open.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected Open, got %v", cb.CurrentState())
	}

	// Advance time past OpenDuration.
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))

	// The first Allow() after the duration elapses should transition and return true.
	if !cb.Allow() {
		t.Error("Allow() = false after OpenDuration elapsed, want true (HalfOpen probe)")
	}
	if cb.CurrentState() != StateHalfOpen {
		t.Errorf("state = %v after OpenDuration elapsed, want HalfOpen", cb.CurrentState())
	}
}

func TestAllowWhenHalfOpen(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Drive to Open, then HalfOpen.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))
	_ = cb.Allow() // transitions to HalfOpen and consumes the single probe

	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.CurrentState())
	}

	// A second Allow() in HalfOpen must be blocked.
	if cb.Allow() {
		t.Error("Allow() = true for second call in HalfOpen, want false")
	}
}

// --------------------------------------------------------------------------
// RecordSuccess
// --------------------------------------------------------------------------

func TestRecordSuccessInClosed(t *testing.T) {
	cb := New("test-provider", testLogger())
	// Accumulate some failures (below threshold so state stays Closed).
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.CurrentState() != StateClosed {
		t.Fatalf("expected Closed after sub-threshold failures, got %v", cb.CurrentState())
	}

	// Success should reset counters.
	cb.RecordSuccess()

	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after RecordSuccess in Closed, want Closed", cb.CurrentState())
	}
	// After reset, two more failures should still not open (threshold is 3).
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after 2 failures post-success, want Closed", cb.CurrentState())
	}
}

func TestRecordSuccessInHalfOpen(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Drive: Closed → Open → HalfOpen.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))
	_ = cb.Allow() // probe → HalfOpen

	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.CurrentState())
	}

	cb.RecordSuccess()

	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after RecordSuccess in HalfOpen, want Closed", cb.CurrentState())
	}
}

// --------------------------------------------------------------------------
// RecordFailure
// --------------------------------------------------------------------------

func TestRecordFailureBelowThreshold(t *testing.T) {
	cb := New("test-provider", testLogger())
	// Record threshold-1 failures.
	for i := 0; i < cb.FailureThreshold-1; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after %d failures, want Closed", cb.CurrentState(), cb.FailureThreshold-1)
	}
}

func TestRecordFailureReachesThreshold(t *testing.T) {
	cb := New("test-provider", testLogger())
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateOpen {
		t.Errorf("state = %v after %d failures, want Open", cb.CurrentState(), cb.FailureThreshold)
	}
}

func TestRecordFailureInHalfOpen(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Closed → Open → HalfOpen.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))
	_ = cb.Allow()

	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.CurrentState())
	}

	cb.RecordFailure()

	if cb.CurrentState() != StateOpen {
		t.Errorf("state = %v after RecordFailure in HalfOpen, want Open", cb.CurrentState())
	}
}

func TestRecordFailureOutsideWindow(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Record threshold-1 failures at base time (all inside window).
	for i := 0; i < cb.FailureThreshold-1; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateClosed {
		t.Fatalf("expected Closed, got %v", cb.CurrentState())
	}

	// Advance clock past the FailureWindow — old failures should expire.
	cb.now = fixedNow(base.Add(cb.FailureWindow + time.Second))
	cb.RecordFailure() // this is now the first failure in the new window

	// Should NOT have tripped because counter was reset.
	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after failure outside window, want Closed (counter should reset)", cb.CurrentState())
	}
}

// --------------------------------------------------------------------------
// Integration scenarios
// --------------------------------------------------------------------------

func TestFullLifecycle(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Phase 1: 3 failures → Open.
	for i := 0; i < cb.FailureThreshold; i++ {
		if !cb.Allow() {
			t.Fatalf("Allow() = false in Closed state, iteration %d", i)
		}
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected Open, got %v", cb.CurrentState())
	}

	// Phase 2: Open blocks requests.
	if cb.Allow() {
		t.Error("Allow() = true in Open state, want false")
	}

	// Phase 3: Wait → HalfOpen.
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))
	if !cb.Allow() {
		t.Error("Allow() = false after OpenDuration elapsed, want true (probe)")
	}
	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.CurrentState())
	}

	// Phase 4: Probe succeeds → Closed.
	cb.RecordSuccess()
	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after probe success, want Closed", cb.CurrentState())
	}

	// Phase 5: Back to normal.
	if !cb.Allow() {
		t.Error("Allow() = false after recovery to Closed, want true")
	}
}

func TestFullLifecycleWithProbeFailure(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// 3 failures → Open.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected Open, got %v", cb.CurrentState())
	}

	// First OpenDuration elapses → probe.
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))
	if !cb.Allow() {
		t.Fatal("probe Allow() = false, want true")
	}
	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.CurrentState())
	}

	// Probe fails → back to Open.
	reopenTime := cb.now()
	cb.RecordFailure()
	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected Open after probe failure, got %v", cb.CurrentState())
	}

	// Requests still blocked.
	if cb.Allow() {
		t.Error("Allow() = true after probe failure re-open, want false")
	}

	// Second OpenDuration elapses → second probe.
	cb.now = fixedNow(reopenTime.Add(cb.OpenDuration + time.Second))
	if !cb.Allow() {
		t.Fatal("second probe Allow() = false, want true")
	}
	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen on second probe, got %v", cb.CurrentState())
	}

	// Second probe succeeds → Closed.
	cb.RecordSuccess()
	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after second probe success, want Closed", cb.CurrentState())
	}
}

// --------------------------------------------------------------------------
// Concurrency
// --------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	cb := New("test-provider", testLogger())
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch j % 3 {
				case 0:
					_ = cb.Allow()
				case 1:
					cb.RecordFailure()
				case 2:
					cb.RecordSuccess()
				}
			}
		}(i)
	}

	wg.Wait()
	// If we reach here without a panic or data race the test passes.
	// Run with -race to catch data races.
	_ = cb.CurrentState()
}

// --------------------------------------------------------------------------
// Configuration
// --------------------------------------------------------------------------

func TestNewWithConfig(t *testing.T) {
	cfg := Config{
		FailureThreshold: 5,
		FailureWindow:    2 * time.Minute,
		OpenDuration:     10 * time.Second,
	}
	cb := NewWithConfig("provider-x", cfg, testLogger())

	if cb.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", cb.FailureThreshold)
	}
	if cb.FailureWindow != 2*time.Minute {
		t.Errorf("FailureWindow = %v, want 2m", cb.FailureWindow)
	}
	if cb.OpenDuration != 10*time.Second {
		t.Errorf("OpenDuration = %v, want 10s", cb.OpenDuration)
	}
	if cb.CurrentState() != StateClosed {
		t.Errorf("initial state = %v, want Closed", cb.CurrentState())
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d, want 3", cfg.FailureThreshold)
	}
	if cfg.FailureWindow != time.Minute {
		t.Errorf("FailureWindow = %v, want 1m", cfg.FailureWindow)
	}
	if cfg.OpenDuration != 30*time.Second {
		t.Errorf("OpenDuration = %v, want 30s", cfg.OpenDuration)
	}
}

func TestReset(t *testing.T) {
	cb := New("test-provider", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Drive to Open.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected Open before Reset, got %v", cb.CurrentState())
	}

	cb.Reset()

	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after Reset, want Closed", cb.CurrentState())
	}
	if !cb.Allow() {
		t.Error("Allow() = false after Reset, want true")
	}

	// Verify counter reset: threshold-1 failures should not trip.
	for i := 0; i < cb.FailureThreshold-1; i++ {
		cb.RecordFailure()
	}
	if cb.CurrentState() != StateClosed {
		t.Errorf("state = %v after %d post-reset failures, want Closed", cb.CurrentState(), cb.FailureThreshold-1)
	}
}

// --------------------------------------------------------------------------
// State.String
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// RecordRateLimitWithCooldown + CooldownRemaining
// --------------------------------------------------------------------------

func TestRecordRateLimitWithCooldown(t *testing.T) {
	cb := New("test", testLogger())

	// Record a rate limit with 60s cooldown.
	cb.RecordRateLimitWithCooldown(60 * time.Second)

	if cb.CurrentState() != StateOpen {
		t.Fatal("should be open after rate limit cooldown")
	}

	// Should not allow requests immediately.
	if cb.Allow() {
		t.Error("should not allow during cooldown")
	}

	// Remaining cooldown should be approximately 60s.
	remaining := cb.CooldownRemaining()
	if remaining < 59*time.Second || remaining > 61*time.Second {
		t.Errorf("CooldownRemaining = %v, want ~60s", remaining)
	}
}

func TestCooldownRespectsMinimumOpenDuration(t *testing.T) {
	cb := New("test", testLogger())
	// OpenDuration default is 30s. If Retry-After is only 5s,
	// the cooldown should be 30s (the longer of the two).
	cb.RecordRateLimitWithCooldown(5 * time.Second)

	if cb.CurrentState() != StateOpen {
		t.Fatal("should be open")
	}

	// Cooldown should be at least OpenDuration (30s), not 5s.
	remaining := cb.CooldownRemaining()
	if remaining < 29*time.Second {
		t.Errorf("CooldownRemaining = %v, want >= 29s (respects OpenDuration)", remaining)
	}
}

func TestCooldownClearsOnTransition(t *testing.T) {
	now := time.Now()
	cb := New("test", testLogger())
	cb.now = func() time.Time { return now }

	cb.RecordRateLimitWithCooldown(10 * time.Second)

	// Advance time past cooldown (OpenDuration=30s wins since 10s < 30s).
	cb.now = func() time.Time { return now.Add(35 * time.Second) }

	// Allow should return true (transition to half-open).
	if !cb.Allow() {
		t.Fatal("should allow after cooldown expires")
	}
	if cb.CurrentState() != StateHalfOpen {
		t.Errorf("state = %v, want half-open", cb.CurrentState())
	}

	// CooldownRemaining should be 0 after clear.
	if r := cb.CooldownRemaining(); r != 0 {
		t.Errorf("CooldownRemaining = %v, want 0", r)
	}
}

func TestCooldownLongerThanOpenDuration(t *testing.T) {
	now := time.Now()
	cb := New("test", testLogger())
	cb.now = func() time.Time { return now }

	// Cooldown = 60s, which is longer than OpenDuration (30s).
	cb.RecordRateLimitWithCooldown(60 * time.Second)

	// After 35s (past OpenDuration but not past cooldown), should still be blocked.
	cb.now = func() time.Time { return now.Add(35 * time.Second) }
	if cb.Allow() {
		t.Error("should not allow before cooldown expires (60s)")
	}

	// After 60s, should allow.
	cb.now = func() time.Time { return now.Add(60 * time.Second) }
	if !cb.Allow() {
		t.Fatal("should allow after 60s cooldown expires")
	}
	if cb.CurrentState() != StateHalfOpen {
		t.Errorf("state = %v, want half-open", cb.CurrentState())
	}
}

func TestCooldownRemainingWhenNotInCooldown(t *testing.T) {
	cb := New("test", testLogger())
	if r := cb.CooldownRemaining(); r != 0 {
		t.Errorf("CooldownRemaining = %v, want 0 when not in cooldown", r)
	}
}

// --------------------------------------------------------------------------
// FailureWeight
// --------------------------------------------------------------------------

func TestFailureWeight(t *testing.T) {
	tests := []struct {
		reason string
		want   int
	}{
		{"rate_limit", 1},
		{"rate_limit_tokens_exhausted", 1},
		{"overloaded", 1},
		{"server_error", 2},
		{"network", 2},
		{"ttft_timeout", 3},
		{"auth", 0},
		{"context_overflow", 0},
		{"client_error", 0},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := FailureWeight(tt.reason)
			if got != tt.want {
				t.Errorf("FailureWeight(%q) = %d, want %d", tt.reason, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// RecordFailureWithReason
// --------------------------------------------------------------------------

func TestRecordFailureWithReason_RateLimitSlower(t *testing.T) {
	// Rate limits (weight 1) need 3 occurrences to trip threshold=3.
	cb := New("test", testLogger())
	cb.RecordFailureWithReason("rate_limit")
	cb.RecordFailureWithReason("rate_limit")

	if cb.CurrentState() != StateClosed {
		t.Error("2 rate limits should not open circuit (threshold=3)")
	}

	cb.RecordFailureWithReason("rate_limit")
	if cb.CurrentState() != StateOpen {
		t.Error("3 rate limits should open circuit")
	}
}

func TestRecordFailureWithReason_ServerErrorFaster(t *testing.T) {
	// Server errors (weight 2) need only 2 occurrences to trip threshold=3.
	cb := New("test", testLogger())
	cb.RecordFailureWithReason("server_error")

	if cb.CurrentState() != StateClosed {
		t.Error("1 server error should not open circuit")
	}

	cb.RecordFailureWithReason("server_error") // total weight = 4 >= 3
	if cb.CurrentState() != StateOpen {
		t.Error("2 server errors (weight 4) should open circuit (threshold=3)")
	}
}

func TestRecordFailureWithReason_FatalIgnored(t *testing.T) {
	// Fatal errors (weight 0) should never open the circuit.
	cb := New("test", testLogger())
	for i := 0; i < 10; i++ {
		cb.RecordFailureWithReason("auth")
	}
	if cb.CurrentState() != StateClosed {
		t.Error("fatal errors should not affect circuit state")
	}
}

func TestRecordFailureWithReason_TTFTTripsQuickly(t *testing.T) {
	// TTFT timeout (weight 3) trips immediately on first occurrence.
	cb := New("test", testLogger())
	cb.RecordFailureWithReason("ttft_timeout") // weight 3 >= threshold 3
	if cb.CurrentState() != StateOpen {
		t.Error("single TTFT timeout (weight 3) should open circuit (threshold=3)")
	}
}

func TestRecordFailureWithReason_HalfOpenReopens(t *testing.T) {
	cb := New("test", testLogger())
	base := time.Now()
	cb.now = fixedNow(base)

	// Drive to Open → HalfOpen.
	for i := 0; i < cb.FailureThreshold; i++ {
		cb.RecordFailure()
	}
	cb.now = fixedNow(base.Add(cb.OpenDuration + time.Second))
	_ = cb.Allow() // → HalfOpen

	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.CurrentState())
	}

	cb.RecordFailureWithReason("server_error")
	if cb.CurrentState() != StateOpen {
		t.Error("RecordFailureWithReason in HalfOpen should reopen circuit")
	}
}

// --------------------------------------------------------------------------
// State.String
// --------------------------------------------------------------------------

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}
