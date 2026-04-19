package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --------------------------------------------------------------------------
// GenericOpenAIProvider — constructor and metadata
// --------------------------------------------------------------------------

func TestGenericProviderConstructor(t *testing.T) {
	models := []string{"gpt-4o", "gpt-4o-mini"}
	p := NewGenericOpenAIProvider(
		"openai", "OpenAI",
		"https://api.openai.com", "sk-test",
		AuthTypeBearer, models, nil, discardLogger(),
	)
	if p == nil {
		t.Fatal("NewGenericOpenAIProvider() returned nil")
	}
}

func TestGenericProviderID(t *testing.T) {
	p := NewGenericOpenAIProvider("myid", "MyName", "https://example.com", "key", AuthTypeBearer, nil, nil, discardLogger())
	if got := p.ID(); got != "myid" {
		t.Errorf("ID() = %q, want %q", got, "myid")
	}
}

func TestGenericProviderName(t *testing.T) {
	p := NewGenericOpenAIProvider("myid", "MyName", "https://example.com", "key", AuthTypeBearer, nil, nil, discardLogger())
	if got := p.Name(); got != "MyName" {
		t.Errorf("Name() = %q, want %q", got, "MyName")
	}
}

func TestGenericProviderBaseURL(t *testing.T) {
	p := NewGenericOpenAIProvider("myid", "MyName", "https://example.com", "key", AuthTypeBearer, nil, nil, discardLogger())
	if got := p.BaseURL(); got != "https://example.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://example.com")
	}
}

// --------------------------------------------------------------------------
// IsAvailable
// --------------------------------------------------------------------------

func TestGenericProviderIsAvailable_BearerWithKey(t *testing.T) {
	p := NewGenericOpenAIProvider("id", "Name", "https://x.com", "sk-key", AuthTypeBearer, nil, nil, discardLogger())
	if !p.IsAvailable() {
		t.Error("IsAvailable() = false with bearer+key, want true")
	}
}

func TestGenericProviderIsAvailable_BearerEmptyKey(t *testing.T) {
	p := NewGenericOpenAIProvider("id", "Name", "https://x.com", "", AuthTypeBearer, nil, nil, discardLogger())
	if p.IsAvailable() {
		t.Error("IsAvailable() = true with bearer+empty key, want false")
	}
}

func TestGenericProviderIsAvailable_None(t *testing.T) {
	p := NewGenericOpenAIProvider("ollama", "Ollama", "http://localhost:11434", "", AuthTypeNone, nil, nil, discardLogger())
	if !p.IsAvailable() {
		t.Error("IsAvailable() = false for AuthTypeNone, want true (no key needed)")
	}
}

// --------------------------------------------------------------------------
// SupportsModel
// --------------------------------------------------------------------------

func TestGenericProviderSupportsModel(t *testing.T) {
	p := NewGenericOpenAIProvider("id", "Name", "https://x.com", "key", AuthTypeBearer,
		[]string{"model-a", "model-b"}, nil, discardLogger())

	tests := []struct {
		model string
		want  bool
	}{
		{"model-a", true},
		{"model-b", true},
		{"model-c", false},
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
// ClassifyError — default and custom classifier
// --------------------------------------------------------------------------

func TestGenericProviderClassifyError_Default(t *testing.T) {
	p := NewGenericOpenAIProvider("id", "Name", "https://x.com", "key", AuthTypeBearer, nil, nil, discardLogger())

	got := p.ClassifyError(429, http.Header{}, nil)
	if !got.IsRetriable() {
		t.Error("429 should be retriable")
	}
	if got.Reason != "rate_limit" {
		t.Errorf("Reason = %q, want %q", got.Reason, "rate_limit")
	}
}

func TestGenericProviderClassifyError_Custom(t *testing.T) {
	customClassifier := func(status int, headers http.Header, body []byte) ErrorClassification {
		return ErrorClassification{Type: ErrorFatal, Reason: "custom", StatusCode: status}
	}
	p := NewGenericOpenAIProvider("id", "Name", "https://x.com", "key", AuthTypeBearer, nil, customClassifier, discardLogger())

	got := p.ClassifyError(429, http.Header{}, nil)
	if got.IsRetriable() {
		t.Error("custom classifier returned fatal but got retriable")
	}
	if got.Reason != "custom" {
		t.Errorf("Reason = %q, want %q", got.Reason, "custom")
	}
}

// --------------------------------------------------------------------------
// buildRequest — bearer vs none
// --------------------------------------------------------------------------

func TestGenericProviderBuildRequest_Bearer(t *testing.T) {
	p := NewGenericOpenAIProvider("id", "Name", "https://api.example.com", "mykey", AuthTypeBearer, nil, nil, discardLogger())
	req := &ProxyRequest{RawBody: []byte(`{}`)}

	httpReq, err := p.buildRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if auth := httpReq.Header.Get("Authorization"); auth != "Bearer mykey" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer mykey")
	}
	if ct := httpReq.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestGenericProviderBuildRequest_None(t *testing.T) {
	p := NewGenericOpenAIProvider("ollama", "Ollama", "http://localhost:11434", "", AuthTypeNone, nil, nil, discardLogger())
	req := &ProxyRequest{RawBody: []byte(`{}`)}

	httpReq, err := p.buildRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if auth := httpReq.Header.Get("Authorization"); auth != "" {
		t.Errorf("Authorization = %q, want empty (no auth)", auth)
	}
}

// --------------------------------------------------------------------------
// Send — 200, 400, 500
// --------------------------------------------------------------------------

func TestGenericProviderSend_200(t *testing.T) {
	responseBody := `{"id":"cmpl-123","choices":[{"message":{"content":"Hi"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	p := NewGenericOpenAIProvider("test", "Test", server.URL, "test-key", AuthTypeBearer, nil, nil, discardLogger())
	req := &ProxyRequest{
		RawBody: []byte(`{"model":"test","messages":[]}`),
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

func TestGenericProviderSend_400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	p := NewGenericOpenAIProvider("test", "Test", server.URL, "key", AuthTypeBearer, nil, nil, discardLogger())
	req := &ProxyRequest{RawBody: []byte(`{}`)}

	_, err := p.Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send() should return error for 400")
	}
	var perr *ProviderError
	if !isProviderError(err, &perr) {
		t.Fatalf("error is not *ProviderError: %T", err)
	}
	if perr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", perr.StatusCode)
	}
}

func TestGenericProviderSend_500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	p := NewGenericOpenAIProvider("test", "Test", server.URL, "key", AuthTypeBearer, nil, nil, discardLogger())
	req := &ProxyRequest{RawBody: []byte(`{}`)}

	_, err := p.Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send() should return error for 500")
	}
	var perr *ProviderError
	if !isProviderError(err, &perr) {
		t.Fatalf("error is not *ProviderError: %T", err)
	}
	if perr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", perr.StatusCode)
	}
}

// --------------------------------------------------------------------------
// SendStream
// --------------------------------------------------------------------------

func TestGenericProviderSendStream(t *testing.T) {
	ssePayload := "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	p := NewGenericOpenAIProvider("test", "Test", server.URL, "key", AuthTypeBearer, nil, nil, discardLogger())
	req := &ProxyRequest{
		Stream:  true,
		RawBody: []byte(`{"model":"test","messages":[],"stream":true}`),
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

	ev, err = parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.Data != "[DONE]" {
		t.Errorf("Data = %q, want %q", ev.Data, "[DONE]")
	}
}

func TestGenericProviderSendStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	p := NewGenericOpenAIProvider("test", "Test", server.URL, "key", AuthTypeBearer, nil, nil, discardLogger())
	req := &ProxyRequest{
		Stream:  true,
		RawBody: []byte(`{"model":"test","messages":[],"stream":true}`),
	}

	_, err := p.SendStream(context.Background(), req)
	if err == nil {
		t.Fatal("SendStream() should return error for 401")
	}
	var perr *ProviderError
	if !isProviderError(err, &perr) {
		t.Fatalf("error is not *ProviderError: %T", err)
	}
	if perr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", perr.StatusCode)
	}
}

// --------------------------------------------------------------------------
// Implements Provider interface
// --------------------------------------------------------------------------

func TestGenericProviderImplementsProvider(t *testing.T) {
	var _ Provider = NewGenericOpenAIProvider("id", "Name", "https://x.com", "key", AuthTypeBearer, nil, nil, discardLogger())
}
