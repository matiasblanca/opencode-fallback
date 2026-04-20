package fallback

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --------------------------------------------------------------------------
// Mock provider for testing
// --------------------------------------------------------------------------

type mockProvider struct {
	id         string
	available  bool
	sendFunc   func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error)
	streamFunc func(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error)
}

func (m *mockProvider) ID() string      { return m.id }
func (m *mockProvider) Name() string    { return m.id }
func (m *mockProvider) BaseURL() string { return "http://mock" }
func (m *mockProvider) IsAvailable() bool { return m.available }
func (m *mockProvider) SupportsModel(string) bool { return true }
func (m *mockProvider) ClassifyError(status int, headers http.Header, body []byte) provider.ErrorClassification {
	if status == 429 {
		return provider.ErrorClassification{Type: provider.ErrorRetriable, Reason: "rate_limit", StatusCode: status}
	}
	if status == 401 {
		return provider.ErrorClassification{Type: provider.ErrorFatal, Reason: "auth", StatusCode: status}
	}
	return provider.ErrorClassification{Type: provider.ErrorRetriable, Reason: "server_error", StatusCode: status}
}

func (m *mockProvider) Send(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, req)
	}
	return &provider.ProxyResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)}, nil
}

func (m *mockProvider) SendStream(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	return nil, fmt.Errorf("not implemented")
}

// --------------------------------------------------------------------------
// FallbackChain — basic
// --------------------------------------------------------------------------

func TestFallbackChainFirstProviderSucceeds(t *testing.T) {
	p1 := &mockProvider{id: "p1", available: true}
	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": circuit.New("p1", discardLogger())},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Provider != "p1" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p1")
	}
	if len(result.Failures) != 0 {
		t.Errorf("Failures = %d, want 0", len(result.Failures))
	}
}

func TestFallbackChainFallsToSecondProvider(t *testing.T) {
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
		},
	}
	p2 := &mockProvider{id: "p2", available: true}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": circuit.New("p1", discardLogger()),
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success from second provider")
	}
	if result.Provider != "p2" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p2")
	}
	if len(result.Failures) != 1 {
		t.Errorf("Failures = %d, want 1", len(result.Failures))
	}
	if result.Failures[0].ProviderID != "p1" {
		t.Errorf("Failures[0].ProviderID = %q, want %q", result.Failures[0].ProviderID, "p1")
	}
}

func TestFallbackChainAllProvidersExhausted(t *testing.T) {
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 500}
		},
	}
	p2 := &mockProvider{
		id: "p2", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "p2", StatusCode: 500}
		},
	}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": circuit.New("p1", discardLogger()),
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if result.Success {
		t.Fatal("expected failure — all providers exhausted")
	}
	if len(result.Failures) != 2 {
		t.Errorf("Failures = %d, want 2", len(result.Failures))
	}
}

func TestFallbackChainSkipsCircuitOpen(t *testing.T) {
	cb := circuit.New("p1", discardLogger())
	// Force circuit open.
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	p1 := &mockProvider{id: "p1", available: true}
	p2 := &mockProvider{id: "p2", available: true}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": cb,
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success from p2")
	}
	if result.Provider != "p2" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p2")
	}
	if len(result.Failures) != 1 {
		t.Errorf("Failures = %d, want 1", len(result.Failures))
	}
	if result.Failures[0].Reason != "circuit_open" {
		t.Errorf("Reason = %q, want %q", result.Failures[0].Reason, "circuit_open")
	}
}

func TestFallbackChainRecordsFailureInCircuitBreaker(t *testing.T) {
	cb := circuit.New("p1", discardLogger())
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
		},
	}
	p2 := &mockProvider{id: "p2", available: true}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": cb,
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	_ = chain.Execute(context.Background(), req)

	// After one failure, circuit should still be closed (threshold=3).
	if cb.CurrentState() != circuit.StateClosed {
		t.Errorf("circuit state = %v after 1 failure, want Closed", cb.CurrentState())
	}
}

func TestFallbackChainTransportError(t *testing.T) {
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, fmt.Errorf("dial tcp: connection refused")
		},
	}
	p2 := &mockProvider{id: "p2", available: true}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": circuit.New("p1", discardLogger()),
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success from p2 after transport error in p1")
	}
	if result.Provider != "p2" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p2")
	}
}

// --------------------------------------------------------------------------
// FailureRecord
// --------------------------------------------------------------------------

func TestFailureRecordFields(t *testing.T) {
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
		},
	}
	p2 := &mockProvider{id: "p2", available: true}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": circuit.New("p1", discardLogger()),
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)

	req := &provider.ProxyRequest{Model: "model-a", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if len(result.Failures) < 1 {
		t.Fatal("expected at least 1 failure")
	}
	f := result.Failures[0]
	if f.ProviderID != "p1" {
		t.Errorf("ProviderID = %q, want %q", f.ProviderID, "p1")
	}
	if f.ModelID != "model-a" {
		t.Errorf("ModelID = %q, want %q", f.ModelID, "model-a")
	}
	if f.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", f.StatusCode)
	}
	// Duration can be zero on very fast mock calls — just verify it's non-negative.
	if f.Duration < 0 {
		t.Error("Duration should not be negative")
	}
	if f.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// --------------------------------------------------------------------------
// ChainSelector
// --------------------------------------------------------------------------

func TestChainSelectorGlobal(t *testing.T) {
	reg := provider.NewRegistry()
	p1 := &mockProvider{id: "openai", available: true}
	reg.Register(p1)

	selector := NewChainSelector(
		[]ChainConfig{{ProviderID: "openai", ModelID: "gpt-4o"}},
		nil,
		nil,
		reg,
		map[string]*circuit.CircuitBreaker{"openai": circuit.New("openai", discardLogger())},
		discardLogger(),
	)

	chain := selector.SelectChain("anything")
	if chain == nil {
		t.Fatal("SelectChain returned nil")
	}
	if len(chain.providers) != 1 {
		t.Errorf("chain providers = %d, want 1", len(chain.providers))
	}
}

func TestChainSelectorAgentOverride(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&mockProvider{id: "openai", available: true})
	reg.Register(&mockProvider{id: "deepseek", available: true})

	agents := map[string][]ChainConfig{
		"sdd-apply": {
			{ProviderID: "deepseek", ModelID: "deepseek-chat"},
		},
	}

	selector := NewChainSelector(
		[]ChainConfig{{ProviderID: "openai", ModelID: "gpt-4o"}},
		nil,
		agents,
		reg,
		map[string]*circuit.CircuitBreaker{
			"openai":   circuit.New("openai", discardLogger()),
			"deepseek": circuit.New("deepseek", discardLogger()),
		},
		discardLogger(),
	)

	chain := selector.SelectChain("sdd-apply")
	if len(chain.providers) != 1 {
		t.Fatalf("chain providers = %d, want 1", len(chain.providers))
	}
	if chain.providers[0].Provider.ID() != "deepseek" {
		t.Errorf("provider = %q, want %q", chain.providers[0].Provider.ID(), "deepseek")
	}
}

func TestChainSelectorGroupMatch(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&mockProvider{id: "openai", available: true})
	reg.Register(&mockProvider{id: "anthropic", available: true})

	groups := map[string][]ChainConfig{
		"sdd-*": {
			{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		},
	}

	selector := NewChainSelector(
		[]ChainConfig{{ProviderID: "openai", ModelID: "gpt-4o"}},
		groups,
		nil,
		reg,
		map[string]*circuit.CircuitBreaker{
			"openai":    circuit.New("openai", discardLogger()),
			"anthropic": circuit.New("anthropic", discardLogger()),
		},
		discardLogger(),
	)

	chain := selector.SelectChain("sdd-explore")
	if len(chain.providers) != 1 {
		t.Fatalf("chain providers = %d, want 1", len(chain.providers))
	}
	if chain.providers[0].Provider.ID() != "anthropic" {
		t.Errorf("provider = %q, want %q", chain.providers[0].Provider.ID(), "anthropic")
	}
}

func TestChainSelectorAgentTakesPriority(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&mockProvider{id: "openai", available: true})
	reg.Register(&mockProvider{id: "anthropic", available: true})
	reg.Register(&mockProvider{id: "deepseek", available: true})

	groups := map[string][]ChainConfig{
		"sdd-*": {{ProviderID: "anthropic", ModelID: "claude"}},
	}
	agents := map[string][]ChainConfig{
		"sdd-apply": {{ProviderID: "deepseek", ModelID: "deepseek-chat"}},
	}

	selector := NewChainSelector(
		[]ChainConfig{{ProviderID: "openai", ModelID: "gpt-4o"}},
		groups,
		agents,
		reg,
		map[string]*circuit.CircuitBreaker{
			"openai":    circuit.New("openai", discardLogger()),
			"anthropic": circuit.New("anthropic", discardLogger()),
			"deepseek":  circuit.New("deepseek", discardLogger()),
		},
		discardLogger(),
	)

	// "sdd-apply" has agent-specific chain → should use deepseek, not anthropic group.
	chain := selector.SelectChain("sdd-apply")
	if chain.providers[0].Provider.ID() != "deepseek" {
		t.Errorf("provider = %q, want %q (agent takes priority over group)", chain.providers[0].Provider.ID(), "deepseek")
	}
}

func TestMatchGroup(t *testing.T) {
	tests := []struct {
		model   string
		pattern string
		want    bool
	}{
		{"sdd-apply", "sdd-*", true},
		{"sdd-explore", "sdd-*", true},
		{"custom-agent", "sdd-*", false},
		{"sdd", "sdd-*", false},
		{"anything", "*", true},
	}

	for _, tt := range tests {
		t.Run(tt.model+"_"+tt.pattern, func(t *testing.T) {
			got := MatchGroup(tt.model, tt.pattern)
			if got != tt.want {
				t.Errorf("MatchGroup(%q, %q) = %v, want %v", tt.model, tt.pattern, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// FallbackResult
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// TTFT Timeout
// --------------------------------------------------------------------------

func TestTTFTTimeout_HangingStreamFallsToNext(t *testing.T) {
	// Provider 1: opens stream but never sends events (hangs).
	hangingPipe, _ := io.Pipe() // never writes
	p1 := &mockProvider{
		id: "p1", available: true,
		streamFunc: func(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
			return stream.NewSSEParser(hangingPipe), nil
		},
	}

	// Provider 2: opens stream and produces events immediately.
	p2 := &mockProvider{
		id: "p2", available: true,
		streamFunc: func(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
			reader := io.NopCloser(strings.NewReader(body))
			return stream.NewSSEParser(reader), nil
		},
	}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": circuit.New("p1", discardLogger()),
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)
	// Use a very short timeout for testing.
	chain.ttftTimeout = 100 * time.Millisecond

	req := &provider.ProxyRequest{Model: "test", Stream: true, RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success from p2 after TTFT timeout on p1")
	}
	if result.Provider != "p2" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p2")
	}
	if len(result.Failures) != 1 {
		t.Fatalf("Failures = %d, want 1", len(result.Failures))
	}
	if result.Failures[0].Reason != "ttft_timeout" {
		t.Errorf("Reason = %q, want %q", result.Failures[0].Reason, "ttft_timeout")
	}

	// Cleanup: close the hanging pipe.
	hangingPipe.Close()
}

func TestTTFTTimeout_FastStreamSucceeds(t *testing.T) {
	// Provider 1: opens stream and produces events immediately.
	p1 := &mockProvider{
		id: "p1", available: true,
		streamFunc: func(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"fast\"}}]}\n\ndata: [DONE]\n\n"
			reader := io.NopCloser(strings.NewReader(body))
			return stream.NewSSEParser(reader), nil
		},
	}

	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": circuit.New("p1", discardLogger())},
		discardLogger(),
	)
	chain.ttftTimeout = 1 * time.Second

	req := &provider.ProxyRequest{Model: "test", Stream: true, RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Provider != "p1" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p1")
	}
	if len(result.Failures) != 0 {
		t.Errorf("Failures = %d, want 0", len(result.Failures))
	}

	// Verify the first event is preserved via PrefixedParser.
	if result.Stream == nil {
		t.Fatal("Stream should not be nil")
	}
	ev, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.ContentDelta != "fast" {
		t.Errorf("ContentDelta = %q, want %q", ev.ContentDelta, "fast")
	}
}

func TestTTFTTimeout_DisabledPassesThrough(t *testing.T) {
	// With TTFT disabled (0), stream should pass through without TTFT check.
	p1 := &mockProvider{
		id: "p1", available: true,
		streamFunc: func(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"
			reader := io.NopCloser(strings.NewReader(body))
			return stream.NewSSEParser(reader), nil
		},
	}

	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": circuit.New("p1", discardLogger())},
		discardLogger(),
	)
	chain.ttftTimeout = 0 // disabled

	req := &provider.ProxyRequest{Model: "test", Stream: true, RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Provider != "p1" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p1")
	}
}

// --------------------------------------------------------------------------
// FallbackResult
// --------------------------------------------------------------------------

func TestFallbackResultString(t *testing.T) {
	r := FallbackResult{
		Success:  true,
		Provider: "openai",
		ModelID:  "gpt-4o",
		Failures: []FailureRecord{
			{ProviderID: "anthropic", Reason: "rate_limit", Duration: time.Second},
		},
	}

	if !r.Success {
		t.Error("Success should be true")
	}
	if r.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", r.Provider, "openai")
	}
}

// --------------------------------------------------------------------------
// Retry with Backoff
// --------------------------------------------------------------------------

func TestRetry_RetriesBeforeFallingThrough(t *testing.T) {
	// Provider 1: fails twice with 429, then succeeds on 3rd attempt.
	var attempts int32
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n <= 2 {
				return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
			}
			return &provider.ProxyResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)}, nil
		},
	}

	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": circuit.New("p1", discardLogger())},
		discardLogger(),
	)
	chain.maxRetries = 2
	chain.retryBaseDelay = 10 * time.Millisecond // fast for testing
	chain.maxRetryDelay = 50 * time.Millisecond

	req := &provider.ProxyRequest{Model: "test", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success after retries")
	}
	if result.Provider != "p1" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p1")
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", atomic.LoadInt32(&attempts))
	}
}

func TestRetry_FatalErrorSkipsRetry(t *testing.T) {
	// Provider 1: returns 401 (fatal) — should NOT retry, should fall to p2.
	var p1Attempts int32
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			atomic.AddInt32(&p1Attempts, 1)
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 401}
		},
	}
	p2 := &mockProvider{id: "p2", available: true}

	chain := NewChain(
		[]ProviderWithModel{
			{Provider: p1, ModelID: "model-a"},
			{Provider: p2, ModelID: "model-b"},
		},
		map[string]*circuit.CircuitBreaker{
			"p1": circuit.New("p1", discardLogger()),
			"p2": circuit.New("p2", discardLogger()),
		},
		discardLogger(),
	)
	chain.maxRetries = 2

	req := &provider.ProxyRequest{Model: "test", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if !result.Success {
		t.Fatal("expected success from p2")
	}
	if result.Provider != "p2" {
		t.Errorf("Provider = %q, want %q", result.Provider, "p2")
	}
	// p1 should have been called exactly once (no retry on fatal).
	if n := atomic.LoadInt32(&p1Attempts); n != 1 {
		t.Errorf("p1 attempts = %d, want 1", n)
	}
}

func TestRetry_RespectsRetryAfterHeader(t *testing.T) {
	// Verify that retryDelay respects Retry-After.
	chain := &Chain{
		retryBaseDelay: 1 * time.Second,
		maxRetryDelay:  10 * time.Second,
	}

	// With Retry-After = 3s, delay should be 3s (not exponential).
	d := chain.retryDelay(0, 3*time.Second)
	if d != 3*time.Second {
		t.Errorf("delay = %v, want 3s", d)
	}

	// With Retry-After = 0, use exponential: 1s * 2^0 = 1s.
	d = chain.retryDelay(0, 0)
	if d != 1*time.Second {
		t.Errorf("delay = %v, want 1s", d)
	}

	// With Retry-After = 0, attempt 2: 1s * 2^2 = 4s.
	d = chain.retryDelay(2, 0)
	if d != 4*time.Second {
		t.Errorf("delay = %v, want 4s", d)
	}

	// Cap at maxRetryDelay.
	d = chain.retryDelay(10, 0)
	if d != 10*time.Second {
		t.Errorf("delay = %v, want 10s (capped)", d)
	}
}

func TestRetry_NoRetryWhenDisabled(t *testing.T) {
	var attempts int32
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
		},
	}

	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": circuit.New("p1", discardLogger())},
		discardLogger(),
	)
	chain.maxRetries = 0 // disabled

	req := &provider.ProxyRequest{Model: "test", RawBody: []byte(`{}`)}
	result := chain.Execute(context.Background(), req)

	if result.Success {
		t.Fatal("expected failure (no retry, only 1 failing provider)")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
}

func TestRetry_ContextCancelledDuringWait(t *testing.T) {
	var attempts int32
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
		},
	}

	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": circuit.New("p1", discardLogger())},
		discardLogger(),
	)
	chain.maxRetries = 3
	chain.retryBaseDelay = 5 * time.Second // long delay
	chain.maxRetryDelay = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay (while waiting for retry).
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req := &provider.ProxyRequest{Model: "test", RawBody: []byte(`{}`)}
	result := chain.Execute(ctx, req)

	if result.Success {
		t.Fatal("expected failure due to context cancellation")
	}
	// Should have attempted once, then cancelled during retry wait.
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("attempts = %d, want 1 (cancelled during retry wait)", n)
	}
	// Last failure should be context_cancelled.
	last := result.Failures[len(result.Failures)-1]
	if last.Reason != "context_cancelled" {
		t.Errorf("last failure reason = %q, want %q", last.Reason, "context_cancelled")
	}
}

func TestRetry_ShouldRetry(t *testing.T) {
	chain := &Chain{}
	tests := []struct {
		reason string
		want   bool
	}{
		{"rate_limit", true},
		{"rate_limit_tokens_exhausted", true},
		{"overloaded", true},
		{"server_error", true},
		{"network", true},
		{"ttft_timeout", true},
		{"unknown", true},
		{"auth", false},
		{"context_overflow", false},
		{"model_not_found", false},
		{"client_error", false},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := chain.shouldRetry(FailureRecord{Reason: tt.reason})
			if got != tt.want {
				t.Errorf("shouldRetry(%q) = %v, want %v", tt.reason, got, tt.want)
			}
		})
	}
}

func TestRetry_UsesRecordFailureWithReason(t *testing.T) {
	// Verify that after retries exhaust, the circuit breaker records
	// with reason-aware weights instead of plain RecordFailure.
	cb := circuit.New("p1", discardLogger())
	p1 := &mockProvider{
		id: "p1", available: true,
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "p1", StatusCode: 429}
		},
	}

	chain := NewChain(
		[]ProviderWithModel{{Provider: p1, ModelID: "model-a"}},
		map[string]*circuit.CircuitBreaker{"p1": cb},
		discardLogger(),
	)
	chain.maxRetries = 0 // no retry, just test CB recording
	chain.retryBaseDelay = 10 * time.Millisecond

	req := &provider.ProxyRequest{Model: "test", RawBody: []byte(`{}`)}

	// Run chain once — rate_limit has weight 1, threshold=3 → still closed.
	_ = chain.Execute(context.Background(), req)
	if cb.CurrentState() != circuit.StateClosed {
		t.Error("CB should be closed after 1 rate_limit (weight 1, threshold 3)")
	}

	// Run two more times → total weight 3 → should open.
	_ = chain.Execute(context.Background(), req)
	_ = chain.Execute(context.Background(), req)
	if cb.CurrentState() != circuit.StateOpen {
		t.Error("CB should be open after 3 rate_limits (weight 3, threshold 3)")
	}
}
