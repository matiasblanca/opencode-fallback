package provider

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --------------------------------------------------------------------------
// OpenAIProvider — metadata
// --------------------------------------------------------------------------

func TestOpenAIProviderID(t *testing.T) {
	p := NewOpenAIProvider("https://api.openai.com", "test-key", []string{"gpt-4o"}, discardLogger())
	if got := p.ID(); got != "openai" {
		t.Errorf("ID() = %q, want %q", got, "openai")
	}
}

func TestOpenAIProviderName(t *testing.T) {
	p := NewOpenAIProvider("https://api.openai.com", "test-key", []string{"gpt-4o"}, discardLogger())
	if got := p.Name(); got != "OpenAI" {
		t.Errorf("Name() = %q, want %q", got, "OpenAI")
	}
}

func TestOpenAIProviderBaseURL(t *testing.T) {
	p := NewOpenAIProvider("https://api.openai.com", "test-key", []string{"gpt-4o"}, discardLogger())
	if got := p.BaseURL(); got != "https://api.openai.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://api.openai.com")
	}
}

// --------------------------------------------------------------------------
// IsAvailable
// --------------------------------------------------------------------------

func TestOpenAIProviderIsAvailable(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{"with key", "sk-test-123", true},
		{"empty key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewOpenAIProvider("https://api.openai.com", tt.apiKey, []string{"gpt-4o"}, discardLogger())
			if got := p.IsAvailable(); got != tt.want {
				t.Errorf("IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SupportsModel
// --------------------------------------------------------------------------

func TestOpenAIProviderSupportsModel(t *testing.T) {
	p := NewOpenAIProvider("https://api.openai.com", "test-key", []string{"gpt-4o", "gpt-4o-mini"}, discardLogger())

	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", true},
		{"gpt-4o-mini", true},
		{"claude-sonnet-4-20250514", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := p.SupportsModel(tt.model); got != tt.want {
				t.Errorf("SupportsModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ClassifyError (delegates to ClassifyOpenAIError)
// --------------------------------------------------------------------------

func TestOpenAIProviderClassifyError(t *testing.T) {
	p := NewOpenAIProvider("https://api.openai.com", "test-key", []string{"gpt-4o"}, discardLogger())

	got := p.ClassifyError(429, http.Header{}, nil)
	if !got.IsRetriable() {
		t.Error("429 should be retriable")
	}
	if got.Reason != "rate_limit" {
		t.Errorf("Reason = %q, want %q", got.Reason, "rate_limit")
	}
}

// --------------------------------------------------------------------------
// Send — non-streaming (integration with httptest)
// --------------------------------------------------------------------------

func TestOpenAIProviderSendNonStreaming(t *testing.T) {
	responseBody := `{"id":"chatcmpl-123","choices":[{"message":{"role":"assistant","content":"Hello!"}}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers.
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), "application/json")
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Path = %q, want %q", r.URL.Path, "/v1/chat/completions")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	p := NewOpenAIProvider(server.URL, "test-key", []string{"gpt-4o"}, discardLogger())
	req := &ProxyRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
		Stream:   false,
		RawBody:  []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}],"stream":false}`),
	}

	resp, err := p.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != responseBody {
		t.Errorf("Body = %q, want %q", string(resp.Body), responseBody)
	}
}

func TestOpenAIProviderSendErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
	}))
	defer server.Close()

	p := NewOpenAIProvider(server.URL, "test-key", []string{"gpt-4o"}, discardLogger())
	req := &ProxyRequest{
		Model:   "gpt-4o",
		Stream:  false,
		RawBody: []byte(`{"model":"gpt-4o","messages":[],"stream":false}`),
	}

	_, err := p.Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send() should return error for 429")
	}

	// The error should be a ProviderError with classification info.
	var perr *ProviderError
	if !isProviderError(err, &perr) {
		t.Fatalf("error is not a *ProviderError: %T", err)
	}
	if perr.StatusCode != 429 {
		t.Errorf("ProviderError.StatusCode = %d, want 429", perr.StatusCode)
	}
}

// --------------------------------------------------------------------------
// SendStream — streaming (integration with httptest)
// --------------------------------------------------------------------------

func TestOpenAIProviderSendStream(t *testing.T) {
	ssePayload := "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	p := NewOpenAIProvider(server.URL, "test-key", []string{"gpt-4o"}, discardLogger())
	req := &ProxyRequest{
		Model:   "gpt-4o",
		Stream:  true,
		RawBody: []byte(`{"model":"gpt-4o","messages":[],"stream":true}`),
	}

	parser, err := p.SendStream(context.Background(), req)
	if err != nil {
		t.Fatalf("SendStream() error = %v", err)
	}
	defer parser.Close()

	// First event: content delta.
	ev, err := parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.ContentDelta != "Hi" {
		t.Errorf("ContentDelta = %q, want %q", ev.ContentDelta, "Hi")
	}

	// Second event: [DONE].
	ev, err = parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.Data != "[DONE]" {
		t.Errorf("Data = %q, want %q", ev.Data, "[DONE]")
	}
}

// --------------------------------------------------------------------------
// Registry
// --------------------------------------------------------------------------

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	p := NewOpenAIProvider("https://api.openai.com", "key", []string{"gpt-4o"}, discardLogger())
	reg.Register(p)

	got, err := reg.Get("openai")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID() != "openai" {
		t.Errorf("ID() = %q, want %q", got.ID(), "openai")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("Get() should return error for unregistered provider")
	}
}

func TestRegistryLen(t *testing.T) {
	reg := NewRegistry()
	if reg.Len() != 0 {
		t.Errorf("Len() = %d, want 0", reg.Len())
	}
	p := NewOpenAIProvider("https://api.openai.com", "key", []string{"gpt-4o"}, discardLogger())
	reg.Register(p)
	if reg.Len() != 1 {
		t.Errorf("Len() = %d, want 1", reg.Len())
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()
	p := NewOpenAIProvider("https://api.openai.com", "key", []string{"gpt-4o"}, discardLogger())
	reg.Register(p)

	ids := reg.List()
	if len(ids) != 1 || ids[0] != "openai" {
		t.Errorf("List() = %v, want [openai]", ids)
	}
}

// isProviderError checks if err is a *ProviderError and assigns it.
func isProviderError(err error, target **ProviderError) bool {
	if pe, ok := err.(*ProviderError); ok {
		*target = pe
		return true
	}
	return false
}
