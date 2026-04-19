package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --------------------------------------------------------------------------
// AnthropicProvider — metadata
// --------------------------------------------------------------------------

func TestAnthropicProviderID(t *testing.T) {
	p := NewAnthropicProvider("https://api.anthropic.com", "key", []string{"claude-sonnet-4-20250514"}, discardLogger())
	if got := p.ID(); got != "anthropic" {
		t.Errorf("ID() = %q, want %q", got, "anthropic")
	}
}

func TestAnthropicProviderName(t *testing.T) {
	p := NewAnthropicProvider("https://api.anthropic.com", "key", nil, discardLogger())
	if got := p.Name(); got != "Anthropic" {
		t.Errorf("Name() = %q, want %q", got, "Anthropic")
	}
}

func TestAnthropicProviderIsAvailable(t *testing.T) {
	p := NewAnthropicProvider("https://api.anthropic.com", "key", nil, discardLogger())
	if !p.IsAvailable() {
		t.Error("IsAvailable() = false with key, want true")
	}
	p2 := NewAnthropicProvider("https://api.anthropic.com", "", nil, discardLogger())
	if p2.IsAvailable() {
		t.Error("IsAvailable() = true without key, want false")
	}
}

func TestAnthropicProviderSupportsModel(t *testing.T) {
	p := NewAnthropicProvider("https://api.anthropic.com", "key", []string{"claude-sonnet-4-20250514", "claude-haiku-3-5-20241022"}, discardLogger())
	if !p.SupportsModel("claude-sonnet-4-20250514") {
		t.Error("SupportsModel(claude-sonnet-4-20250514) = false, want true")
	}
	if p.SupportsModel("gpt-4o") {
		t.Error("SupportsModel(gpt-4o) = true, want false")
	}
}

func TestAnthropicProviderClassifyError(t *testing.T) {
	p := NewAnthropicProvider("https://api.anthropic.com", "key", nil, discardLogger())
	got := p.ClassifyError(429, http.Header{}, nil)
	if !got.IsRetriable() {
		t.Error("429 should be retriable")
	}
	if got.Reason != "rate_limit" {
		t.Errorf("Reason = %q, want %q", got.Reason, "rate_limit")
	}
}

// --------------------------------------------------------------------------
// Send — non-streaming (with adapter translation)
// --------------------------------------------------------------------------

func TestAnthropicProviderSend(t *testing.T) {
	// Anthropic returns its native format — the provider translates to OpenAI.
	anthropicResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-20250514",
		"content": [{"type":"text","text":"Hello from Claude!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Anthropic-specific headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), "test-key")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header is missing")
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("Path = %q, want %q", r.URL.Path, "/v1/messages")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(anthropicResp))
	}))
	defer server.Close()

	p := NewAnthropicProvider(server.URL, "test-key", []string{"claude-sonnet-4-20250514"}, discardLogger())
	req := &ProxyRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		Stream:  false,
		RawBody: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hello"}],"stream":false}`),
	}

	resp, err := p.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	// The response body should be in OpenAI format (translated by the provider).
	if len(resp.Body) == 0 {
		t.Error("Body is empty")
	}
}

func TestAnthropicProviderSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"Rate limited"}}`))
	}))
	defer server.Close()

	p := NewAnthropicProvider(server.URL, "test-key", []string{"claude-sonnet-4-20250514"}, discardLogger())
	req := &ProxyRequest{
		Model:   "claude-sonnet-4-20250514",
		RawBody: []byte(`{"model":"claude-sonnet-4-20250514","messages":[]}`),
	}

	_, err := p.Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send() should return error for 429")
	}

	var perr *ProviderError
	if !isProviderError(err, &perr) {
		t.Fatalf("error is not a *ProviderError: %T", err)
	}
	if perr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", perr.StatusCode)
	}
}

// --------------------------------------------------------------------------
// SendStream — streaming
// --------------------------------------------------------------------------

func TestAnthropicProviderSendStream(t *testing.T) {
	// Anthropic SSE events — the raw events are forwarded as-is through the
	// SSEParser. Full Anthropic→OpenAI SSE translation is a v0.2 feature.
	// For v0.1, we forward Anthropic's native SSE format.
	ssePayload := "event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	p := NewAnthropicProvider(server.URL, "test-key", []string{"claude-sonnet-4-20250514"}, discardLogger())
	req := &ProxyRequest{
		Model:   "claude-sonnet-4-20250514",
		Stream:  true,
		RawBody: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}],"stream":true}`),
	}

	parser, err := p.SendStream(context.Background(), req)
	if err != nil {
		t.Fatalf("SendStream() error = %v", err)
	}
	defer parser.Close()

	// First event: message_start.
	ev, err := parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.Type != "message_start" {
		t.Errorf("Type = %q, want %q", ev.Type, "message_start")
	}
}
