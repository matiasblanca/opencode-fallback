package fallback

import (
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

// FallbackResult contains the outcome of executing a fallback chain.
// Pattern from Manifest: success + failures[] for full observability.
type FallbackResult struct {
	// Success is true if a provider responded successfully.
	Success bool
	// Provider is the ID of the provider that responded.
	Provider string
	// ModelID is the model that was used.
	ModelID string
	// Response is the non-streaming response (nil for streaming or failure).
	Response *provider.ProxyResponse
	// Stream is the SSE parser for streaming responses (nil for non-streaming).
	Stream *stream.SSEParser
	// Failures contains a record of every failed attempt.
	Failures []FailureRecord
}

// FailureRecord documents a single failed provider attempt.
type FailureRecord struct {
	ProviderID string
	ModelID    string
	Error      error
	StatusCode int
	Reason     string        // "rate_limit", "overloaded", "timeout", "circuit_open", "network", etc.
	RetryAfter time.Duration // from provider's Retry-After header
	Duration   time.Duration
	Timestamp  time.Time
}
