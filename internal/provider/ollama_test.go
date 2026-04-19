package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --------------------------------------------------------------------------
// OllamaProvider — metadata
// --------------------------------------------------------------------------

func TestOllamaProviderID(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", []string{"qwen2.5-coder:32b"}, discardLogger())
	if got := p.ID(); got != "ollama" {
		t.Errorf("ID() = %q, want %q", got, "ollama")
	}
}

func TestOllamaProviderName(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", nil, discardLogger())
	if got := p.Name(); got != "Ollama" {
		t.Errorf("Name() = %q, want %q", got, "Ollama")
	}
}

func TestOllamaProviderIsAvailable(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", nil, discardLogger())
	if !p.IsAvailable() {
		t.Error("IsAvailable() = false, want true (Ollama needs no API key)")
	}
}

func TestOllamaProviderSupportsModel(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", []string{"qwen2.5-coder:32b", "llama3.1:70b"}, discardLogger())
	if !p.SupportsModel("qwen2.5-coder:32b") {
		t.Error("SupportsModel(qwen2.5-coder:32b) = false, want true")
	}
	if p.SupportsModel("gpt-4o") {
		t.Error("SupportsModel(gpt-4o) = true, want false")
	}
}

func TestOllamaProviderClassifyError(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", nil, discardLogger())

	tests := []struct {
		name       string
		status     int
		wantType   ErrorType
		wantReason string
	}{
		{"404 model not found", 404, ErrorFatal, "model_not_found"},
		{"500 server error", 500, ErrorRetriable, "server_error"},
		{"400 bad request", 400, ErrorFatal, "client_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.ClassifyError(tt.status, http.Header{}, nil)
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Send
// --------------------------------------------------------------------------

func TestOllamaProviderSend(t *testing.T) {
	responseBody := `{"id":"ollama-123","choices":[{"message":{"content":"Hello from Ollama"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ollama should NOT have Authorization header.
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization = %q, want empty (Ollama has no auth)", auth)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, []string{"qwen2.5-coder:32b"}, discardLogger())
	req := &ProxyRequest{
		Model:   "qwen2.5-coder:32b",
		RawBody: []byte(`{"model":"qwen2.5-coder:32b","messages":[]}`),
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
// SendStream
// --------------------------------------------------------------------------

func TestOllamaProviderSendStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, []string{"qwen2.5-coder:32b"}, discardLogger())
	req := &ProxyRequest{
		Model:   "qwen2.5-coder:32b",
		Stream:  true,
		RawBody: []byte(`{"model":"qwen2.5-coder:32b","messages":[],"stream":true}`),
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

func TestOllamaProviderSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, []string{"nonexistent"}, discardLogger())
	req := &ProxyRequest{
		Model:   "nonexistent",
		RawBody: []byte(`{"model":"nonexistent","messages":[]}`),
	}

	_, err := p.Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send() should return error for 404")
	}

	var perr *ProviderError
	if !isProviderError(err, &perr) {
		t.Fatalf("error is not a *ProviderError: %T", err)
	}
	if perr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", perr.StatusCode)
	}
}
