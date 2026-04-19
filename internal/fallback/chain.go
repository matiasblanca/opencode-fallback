package fallback

import (
	"context"
	"log/slog"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
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
	providers []ProviderWithModel
	breakers  map[string]*circuit.CircuitBreaker
	logger    *slog.Logger
}

// NewChain creates a fallback chain.
func NewChain(
	providers []ProviderWithModel,
	breakers map[string]*circuit.CircuitBreaker,
	logger *slog.Logger,
) *Chain {
	return &Chain{
		providers: providers,
		breakers:  breakers,
		logger:    logger,
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
				// Stream opened successfully.
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
