package proxy

import (
	"fmt"
	"sync"
	"time"
)

// StatusResponse is the JSON structure returned by GET /v1/status.
type StatusResponse struct {
	Version   string           `json:"version"`
	Uptime    string           `json:"uptime"`
	UptimeSec float64          `json:"uptime_seconds"`
	Providers []ProviderStatus `json:"providers"`
	Recent    []FallbackEvent  `json:"recent_fallbacks"`
	Config    StatusConfig     `json:"config"`
}

// ProviderStatus shows the health of a single provider.
type ProviderStatus struct {
	ID            string `json:"id"`
	CircuitState  string `json:"circuit_state"`            // "closed", "open", "half-open"
	Available     bool   `json:"available"`                // IsAvailable()
	CooldownUntil string `json:"cooldown_until,omitempty"` // ISO8601 timestamp if in cooldown
}

// FallbackEvent records a single fallback occurrence for the status endpoint.
type FallbackEvent struct {
	Timestamp    time.Time `json:"timestamp"`
	FromProvider string    `json:"from_provider"`
	FromModel    string    `json:"from_model"`
	ToProvider   string    `json:"to_provider,omitempty"`
	ToModel      string    `json:"to_model,omitempty"`
	Reason       string    `json:"reason"`
	StatusCode   int       `json:"status_code,omitempty"`
	Success      bool      `json:"success"` // did a fallback eventually succeed?
}

// StatusConfig shows configuration summary (no secrets).
type StatusConfig struct {
	ListenAddr  string `json:"listen_addr"`
	TTFTTimeout string `json:"ttft_timeout"`
	ChainLength int    `json:"chain_length"`
}

// FallbackTracker records recent fallback events for the status endpoint.
// It is safe for concurrent use.
type FallbackTracker struct {
	mu     sync.Mutex
	events []FallbackEvent
	max    int
}

// NewFallbackTracker creates a tracker that keeps the last `max` events.
func NewFallbackTracker(max int) *FallbackTracker {
	return &FallbackTracker{
		events: make([]FallbackEvent, 0, max),
		max:    max,
	}
}

// Record adds a fallback event. If the buffer is full, the oldest event
// is evicted.
func (t *FallbackTracker) Record(event FallbackEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) >= t.max {
		t.events = t.events[1:]
	}
	t.events = append(t.events, event)
}

// Events returns a copy of the recent events (newest last).
func (t *FallbackTracker) Events() []FallbackEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]FallbackEvent, len(t.events))
	copy(out, t.events)
	return out
}

// String returns a summary string for the tracker (for debugging).
func (t *FallbackTracker) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("FallbackTracker(%d/%d events)", len(t.events), t.max)
}
