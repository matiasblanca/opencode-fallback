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
	providers   []ProviderWithModel
	breakers    map[string]*circuit.CircuitBreaker
	logger      *slog.Logger
	ttftTimeout time.Duration // 0 means disabled
}

// NewChain creates a fallback chain.
func NewChain(
	providers []ProviderWithModel,
	breakers map[string]*circuit.CircuitBreaker,
	logger *slog.Logger,
) *Chain {
	return &Chain{
		providers:   providers,
		breakers:    breakers,
		logger:      logger,
		ttftTimeout: DefaultTTFTTimeout,
	}
}

// Execute tries each provider in order until one succeeds.
// It follows the algorithm from the architecture doc §6.1.
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

		// Step 2: Send request.
		start := time.Now()

		var resp *provider.ProxyResponse
		var err error

		if req.Stream {
			parser, sErr := entry.Provider.SendStream(ctx, req)
			if sErr == nil {
				// TTFT check: verify the stream actually produces events.
				if c.ttftTimeout > 0 {
					firstEvent, ttftErr := parser.NextWithTimeout(c.ttftTimeout)
					if ttftErr != nil {
						// Stream opened but hung — treat as failure.
						parser.Close()
						if hasCB {
							breaker.RecordFailure()
						}
						failure := FailureRecord{
							ProviderID: pid,
							ModelID:    mid,
							Error:      ttftErr,
							Reason:     "ttft_timeout",
							Duration:   time.Since(start),
							Timestamp:  time.Now(),
						}
						failures = append(failures, failure)
						c.logger.Warn("stream hung (TTFT timeout)",
							"provider", pid,
							"model", mid,
							"timeout", c.ttftTimeout,
						)
						continue // try next provider
					}

					// First event received — stream is alive.
					// Wrap the parser so the first event isn't lost.
					wrappedParser := stream.NewPrefixedParser(parser, firstEvent)
					if hasCB {
						breaker.RecordSuccess()
					}
					c.logger.Info("stream opened (TTFT verified)",
						"provider", pid,
						"model", mid,
						"duration", time.Since(start),
					)
					return FallbackResult{
						Success:  true,
						Provider: pid,
						ModelID:  mid,
						Stream:   wrappedParser,
						Failures: failures,
					}
				}

				// No TTFT timeout configured — original behavior.
				if hasCB {
					breaker.RecordSuccess()
				}
				c.logger.Info("stream opened",
					"provider", pid,
					"model", mid,
					"duration", time.Since(start),
				)
				return FallbackResult{
					Success:  true,
					Provider: pid,
					ModelID:  mid,
					Stream:   parser,
					Failures: failures,
				}
			}
			err = sErr
		} else {
			resp, err = entry.Provider.Send(ctx, req)
		}

		duration := time.Since(start)

		// Step 3: Handle success.
		if err == nil {
			if hasCB {
				breaker.RecordSuccess()
			}
			c.logger.Info("request succeeded",
				"provider", pid,
				"model", mid,
				"duration", duration,
			)
			return FallbackResult{
				Success:  true,
				Provider: pid,
				ModelID:  mid,
				Response: resp,
				Failures: failures,
			}
		}

		// Step 4: Classify and record failure.
		failure := c.classifyFailure(entry, err, duration)
		failures = append(failures, failure)

		if hasCB {
			breaker.RecordFailure()
		}

		c.logger.Info("provider failed, trying next",
			"provider", pid,
			"model", mid,
			"reason", failure.Reason,
			"status", failure.StatusCode,
			"duration", duration,
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

// classifyFailure creates a FailureRecord from an error, checking for
// ProviderError (HTTP-level) and transport errors (network-level).
func (c *Chain) classifyFailure(entry ProviderWithModel, err error, duration time.Duration) FailureRecord {
	pid := entry.Provider.ID()
	mid := entry.ModelID

	// Check if it's a ProviderError (HTTP error response).
	if perr, ok := err.(*provider.ProviderError); ok {
		classification := entry.Provider.ClassifyError(perr.StatusCode, perr.Headers, perr.Body)
		return FailureRecord{
			ProviderID: pid,
			ModelID:    mid,
			Error:      err,
			StatusCode: perr.StatusCode,
			Reason:     classification.Reason,
			Duration:   duration,
			Timestamp:  time.Now(),
		}
	}

	// Check if it's a transport error.
	if classification, ok := provider.ClassifyTransportError(err); ok {
		return FailureRecord{
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
	return FailureRecord{
		ProviderID: pid,
		ModelID:    mid,
		Error:      err,
		Reason:     "unknown",
		Duration:   duration,
		Timestamp:  time.Now(),
	}
}
