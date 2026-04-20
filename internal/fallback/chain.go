package fallback

import (
	"context"
	"log/slog"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

const (
	// DefaultTTFTTimeout is the default time to wait for the first SSE
	// event after a stream is opened. If no event arrives, the stream
	// is considered dead and the next provider is tried.
	//
	// Source: youngbinkim0/opencode-fallback uses 30s.
	// We use 15s because our proxy adds minimal overhead.
	DefaultTTFTTimeout = 15 * time.Second

	// DefaultMaxRetries is the maximum number of retries per provider
	// before falling through to the next one in the chain.
	DefaultMaxRetries = 1

	// DefaultRetryBaseDelay is the base delay for exponential backoff.
	// Actual delay = baseDelay * 2^attempt (capped at MaxRetryDelay).
	DefaultRetryBaseDelay = 1 * time.Second

	// DefaultMaxRetryDelay caps the retry delay to prevent excessive waits.
	DefaultMaxRetryDelay = 10 * time.Second
)

// ProviderWithModel binds a provider to the specific model to use in
// the fallback chain.
type ProviderWithModel struct {
	Provider provider.Provider
	ModelID  string
}

// Chain iterates an ordered list of providers until one responds successfully.
// It integrates with circuit breakers to skip providers that are in an Open
// state, and records failure details for observability.
type Chain struct {
	providers      []ProviderWithModel
	breakers       map[string]*circuit.CircuitBreaker
	logger         *slog.Logger
	ttftTimeout    time.Duration // 0 means disabled
	maxRetries     int           // 0 means no retry (v0.7 behavior)
	retryBaseDelay time.Duration
	maxRetryDelay  time.Duration
}

// NewChain creates a fallback chain.
func NewChain(
	providers []ProviderWithModel,
	breakers map[string]*circuit.CircuitBreaker,
	logger *slog.Logger,
) *Chain {
	return &Chain{
		providers:      providers,
		breakers:       breakers,
		logger:         logger,
		ttftTimeout:    DefaultTTFTTimeout,
		maxRetries:     DefaultMaxRetries,
		retryBaseDelay: DefaultRetryBaseDelay,
		maxRetryDelay:  DefaultMaxRetryDelay,
	}
}

// Execute tries each provider in order until one succeeds.
// For each provider, it retries retriable errors up to maxRetries times
// with exponential backoff before falling through to the next provider.
func (c *Chain) Execute(ctx context.Context, req *provider.ProxyRequest) FallbackResult {
	var failures []FailureRecord

	for _, entry := range c.providers {
		pid := entry.Provider.ID()
		mid := entry.ModelID

		// Step 1: Check circuit breaker.
		breaker, hasCB := c.breakers[pid]
		if hasCB && !breaker.Allow() {
			failures = append(failures, FailureRecord{
				ProviderID: pid,
				ModelID:    mid,
				Reason:     "circuit_open",
				Timestamp:  time.Now(),
			})
			c.logger.Info("skipped provider (circuit open)",
				"provider", pid,
				"model", mid,
			)
			continue
		}

		// Step 2: Try provider with retries.
		var lastFailure *FailureRecord
		maxAttempts := 1 + c.maxRetries // 1 original + N retries

		for attempt := 0; attempt < maxAttempts; attempt++ {
			// Wait before retry (not on first attempt).
			if attempt > 0 {
				retryAfter := time.Duration(0)
				if lastFailure != nil {
					retryAfter = lastFailure.RetryAfter
				}
				delay := c.retryDelay(attempt-1, retryAfter)

				c.logger.Info("retrying same provider",
					"provider", pid,
					"model", mid,
					"attempt", attempt+1,
					"delay", delay,
				)

				select {
				case <-time.After(delay):
					// Delay elapsed, proceed with retry.
				case <-ctx.Done():
					// Context cancelled during wait — give up.
					failures = append(failures, FailureRecord{
						ProviderID: pid,
						ModelID:    mid,
						Error:      ctx.Err(),
						Reason:     "context_cancelled",
						Timestamp:  time.Now(),
					})
					return FallbackResult{Success: false, Failures: failures}
				}
			}

			result, failure := c.attemptProvider(ctx, entry, req, breaker, hasCB)
			if result != nil {
				result.Failures = failures
				return *result
			}

			// Attempt failed.
			lastFailure = failure

			// Don't retry fatal errors.
			if !c.shouldRetry(*failure) {
				break
			}
		}

		// All attempts for this provider failed.
		if lastFailure != nil {
			failures = append(failures, *lastFailure)

			// Record to circuit breaker with reason awareness.
			if hasCB {
				if lastFailure.RetryAfter > 0 &&
					(lastFailure.Reason == "rate_limit" || lastFailure.Reason == "rate_limit_tokens_exhausted") {
					breaker.RecordRateLimitWithCooldown(lastFailure.RetryAfter)
				} else {
					breaker.RecordFailureWithReason(lastFailure.Reason)
				}
			}
		}

		reason := "unknown"
		if lastFailure != nil {
			reason = lastFailure.Reason
		}
		c.logger.Info("provider failed, trying next",
			"provider", pid,
			"model", mid,
			"reason", reason,
			"attempts", maxAttempts,
		)
	}

	// All providers exhausted.
	c.logger.Error("all providers exhausted",
		"failures", len(failures),
	)

	return FallbackResult{
		Success:  false,
		Failures: failures,
	}
}

// attemptProvider makes a single request attempt to one provider.
// Returns (result, nil) on success, or (nil, failure) on failure.
// Does NOT record to circuit breaker — the caller handles that.
func (c *Chain) attemptProvider(
	ctx context.Context,
	entry ProviderWithModel,
	req *provider.ProxyRequest,
	breaker *circuit.CircuitBreaker,
	hasCB bool,
) (*FallbackResult, *FailureRecord) {
	pid := entry.Provider.ID()
	mid := entry.ModelID
	start := time.Now()

	if req.Stream {
		parser, sErr := entry.Provider.SendStream(ctx, req)
		if sErr == nil {
			// TTFT check: verify the stream actually produces events.
			if c.ttftTimeout > 0 {
				firstEvent, ttftErr := parser.NextWithTimeout(c.ttftTimeout)
				if ttftErr != nil {
					// Stream opened but hung — treat as failure.
					parser.Close()
					return nil, &FailureRecord{
						ProviderID: pid,
						ModelID:    mid,
						Error:      ttftErr,
						Reason:     "ttft_timeout",
						Duration:   time.Since(start),
						Timestamp:  time.Now(),
					}
				}

				// First event received — stream is alive.
				wrappedParser := stream.NewPrefixedParser(parser, firstEvent)
				if hasCB {
					breaker.RecordSuccess()
				}
				return &FallbackResult{
					Success:  true,
					Provider: pid,
					ModelID:  mid,
					Stream:   wrappedParser,
				}, nil
			}

			// No TTFT timeout configured — original behavior.
			if hasCB {
				breaker.RecordSuccess()
			}
			return &FallbackResult{
				Success:  true,
				Provider: pid,
				ModelID:  mid,
				Stream:   parser,
			}, nil
		}
		// Stream open failed — classify below.
		return nil, c.classifyFailureRecord(entry, sErr, time.Since(start))
	}

	// Non-streaming.
	resp, err := entry.Provider.Send(ctx, req)
	if err == nil {
		if hasCB {
			breaker.RecordSuccess()
		}
		return &FallbackResult{
			Success:  true,
			Provider: pid,
			ModelID:  mid,
			Response: resp,
		}, nil
	}

	return nil, c.classifyFailureRecord(entry, err, time.Since(start))
}

// retryDelay calculates the backoff delay for a given attempt number.
// Uses exponential backoff: baseDelay * 2^attempt, capped at maxRetryDelay.
// If the error has a Retry-After header, that value takes precedence.
func (c *Chain) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		// Respect provider's Retry-After, but cap it.
		if retryAfter > c.maxRetryDelay {
			return c.maxRetryDelay
		}
		return retryAfter
	}
	delay := c.retryBaseDelay * time.Duration(1<<uint(attempt))
	if delay > c.maxRetryDelay {
		delay = c.maxRetryDelay
	}
	return delay
}

// shouldRetry reports whether a failure should be retried on the SAME provider.
// Fatal errors (auth, context_overflow, model_not_found) are never retried.
// Retriable errors (rate_limit, overloaded, server_error, network) get retried.
func (c *Chain) shouldRetry(failure FailureRecord) bool {
	switch failure.Reason {
	case "rate_limit", "rate_limit_tokens_exhausted", "overloaded",
		"server_error", "network", "ttft_timeout", "unknown":
		return true
	default:
		// Fatal: auth, context_overflow, model_not_found, client_error
		return false
	}
}

// classifyFailureRecord creates a FailureRecord from an error, checking for
// ProviderError (HTTP-level) and transport errors (network-level).
func (c *Chain) classifyFailureRecord(entry ProviderWithModel, err error, duration time.Duration) *FailureRecord {
	pid := entry.Provider.ID()
	mid := entry.ModelID

	// Check if it's a ProviderError (HTTP error response).
	if perr, ok := err.(*provider.ProviderError); ok {
		classification := entry.Provider.ClassifyError(perr.StatusCode, perr.Headers, perr.Body)
		return &FailureRecord{
			ProviderID: pid,
			ModelID:    mid,
			Error:      err,
			StatusCode: perr.StatusCode,
			Reason:     classification.Reason,
			RetryAfter: classification.RetryAfter,
			Duration:   duration,
			Timestamp:  time.Now(),
		}
	}

	// Check if it's a transport error.
	if classification, ok := provider.ClassifyTransportError(err); ok {
		return &FailureRecord{
			ProviderID: pid,
			ModelID:    mid,
			Error:      err,
			StatusCode: classification.StatusCode,
			Reason:     classification.Reason,
			Duration:   duration,
			Timestamp:  time.Now(),
		}
	}

	// Unknown error — treat as retriable.
	return &FailureRecord{
		ProviderID: pid,
		ModelID:    mid,
		Error:      err,
		Reason:     "unknown",
		Duration:   duration,
		Timestamp:  time.Now(),
	}
}


