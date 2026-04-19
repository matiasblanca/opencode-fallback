package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --------------------------------------------------------------------------
// DeepSeekProvider — metadata
// --------------------------------------------------------------------------

func TestDeepSeekProviderID(t *testing.T) {
	p := NewDeepSeekProvider("https://api.deepseek.com", "key", []string{"deepseek-chat"}, discardLogger())
	if got := p.ID(); got != "deepseek" {
		t.Errorf("ID() = %q, want %q", got, "deepseek")
	}
}

func TestDeepSeekProviderName(t *testing.T) {
	p := NewDeepSeekProvider("https://api.deepseek.com", "key", []string{"deepseek-chat"}, discardLogger())
	if got := p.Name(); got != "DeepSeek" {
		t.Errorf("Name() = %q, want %q", got, "DeepSeek")
	}
}

func TestDeepSeekProviderIsAvailable(t *testing.T) {
	p := NewDeepSeekProvider("https://api.deepseek.com", "key", nil, discardLogger())
	if !p.IsAvailable() {
		t.Error("IsAvailable() = false with key, want true")
	}
	p2 := NewDeepSeekProvider("https://api.deepseek.com", "", nil, discardLogger())
	if p2.IsAvailable() {
		t.Error("IsAvailable() = true without key, want false")
	}
}

func TestDeepSeekProviderSupportsModel(t *testing.T) {
	p := NewDeepSeekProvider("https://api.deepseek.com", "key", []string{"deepseek-chat", "deepseek-reasoner"}, discardLogger())
	if !p.SupportsModel("deepseek-chat") {
		t.Error("SupportsModel(deepseek-chat) = false, want true")
	}
	if p.SupportsModel("gpt-4o") {
		t.Error("SupportsModel(gpt-4o) = true, want false")
	}
}

func TestDeepSeekProviderClassifyError(t *testing.T) {
	p := NewDeepSeekProvider("https://api.deepseek.com", "key", nil, discardLogger())
	got := p.ClassifyError(429, http.Header{}, nil)
	if !got.IsRetriable() {
		t.Error("429 should be retriable")
	}
}

// --------------------------------------------------------------------------
// Send — non-streaming
// --------------------------------------------------------------------------

func TestDeepSeekProviderSend(t *testing.T) {
	responseBody := `{"id":"ds-123","choices":[{"message":{"content":"Hello"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	p := NewDeepSeekProvider(server.URL, "test-key", []string{"deepseek-chat"}, discardLogger())
	req := &ProxyRequest{
		Model:   "deepseek-chat",
		RawBody: []byte(`{"model":"deepseek-chat","messages":[]}`),
	}

	resp, err := p.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

// --------------------------------------------------------------------------
// SendStream — streaming
// --------------------------------------------------------------------------

func TestDeepSeekProviderSendStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	p := NewDeepSeekProvider(server.URL, "test-key", []string{"deepseek-chat"}, discardLogger())
	req := &ProxyRequest{
		Model:   "deepseek-chat",
		Stream:  true,
		RawBody: []byte(`{"model":"deepseek-chat","messages":[],"stream":true}`),
	}

	parser, err := p.SendStream(context.Background(), req)
	if err != nil {
		t.Fatalf("SendStream() error = %v", err)
	}
	defer parser.Close()

	ev, err := parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.ContentDelta != "Hi" {
		t.Errorf("ContentDelta = %q, want %q", ev.ContentDelta, "Hi")
	}
}
