package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/fallback"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --------------------------------------------------------------------------
// Mock provider
// --------------------------------------------------------------------------

type mockProvider struct {
	id       string
	sendFunc func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error)
}

func (m *mockProvider) ID() string      { return m.id }
func (m *mockProvider) Name() string    { return m.id }
func (m *mockProvider) BaseURL() string { return "http://mock" }
func (m *mockProvider) IsAvailable() bool { return true }
func (m *mockProvider) SupportsModel(string) bool { return true }
func (m *mockProvider) ClassifyError(status int, headers http.Header, body []byte) provider.ErrorClassification {
	return provider.ErrorClassification{Type: provider.ErrorRetriable, Reason: "mock", StatusCode: status}
}
func (m *mockProvider) Send(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, req)
	}
	return &provider.ProxyResponse{
		StatusCode: 200,
		Body:       []byte(`{"id":"test","choices":[{"message":{"content":"Hello"}}]}`),
	}, nil
}
func (m *mockProvider) SendStream(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
	return nil, fmt.Errorf("not implemented")
}

// --------------------------------------------------------------------------
// Handler tests
// --------------------------------------------------------------------------

func newTestHandler(providers ...provider.Provider) *Handler {
	reg := provider.NewRegistry()
	breakers := make(map[string]*circuit.CircuitBreaker)
	var global []fallback.ChainConfig

	for _, p := range providers {
		reg.Register(p)
		breakers[p.ID()] = circuit.New(p.ID(), discardLogger())
		global = append(global, fallback.ChainConfig{ProviderID: p.ID(), ModelID: "test-model"})
	}

	selector := fallback.NewChainSelector(global, nil, nil, reg, breakers, discardLogger())
	return NewHandler(selector, breakers, reg, discardLogger())
}

func TestHandlerSuccess(t *testing.T) {
	p := &mockProvider{id: "openai"}
	handler := newTestHandler(p)

	body := `{"model":"test-model","messages":[{"role":"user","content":"Hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
}

func TestHandlerWrongMethod(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandlerWrongPath(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	body := `{"model":"test","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/models", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandlerInvalidJSON(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerMissingModel(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerAllProvidersExhausted(t *testing.T) {
	p := &mockProvider{
		id: "openai",
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "openai", StatusCode: 500}
		},
	}
	handler := newTestHandler(p)

	body := `{"model":"test-model","messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 502 {
		t.Errorf("status = %d, want 502", rec.Code)
	}

	// Response should be in OpenAI error format.
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error.Type != "proxy_error" {
		t.Errorf("error type = %q, want %q", errResp.Error.Type, "proxy_error")
	}
}

func TestHandlerFallbackTriggered(t *testing.T) {
	p1 := &mockProvider{
		id: "anthropic",
		sendFunc: func(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
			return nil, &provider.ProviderError{ProviderID: "anthropic", StatusCode: 429}
		},
	}
	p2 := &mockProvider{id: "openai"}

	handler := newTestHandler(p1, p2)

	body := `{"model":"test-model","messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200 (fallback to second provider)", rec.Code)
	}
}

// --------------------------------------------------------------------------
// parseRequest
// --------------------------------------------------------------------------

func TestParseRequest(t *testing.T) {
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}],"stream":true,"temperature":0.7}`
	req, err := parseRequest([]byte(body), http.Header{})
	if err != nil {
		t.Fatalf("parseRequest() error = %v", err)
	}
	if req.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", req.Model, "gpt-4o")
	}
	if !req.Stream {
		t.Error("Stream = false, want true")
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", req.Temperature)
	}
	if len(req.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(req.Messages))
	}
}

func TestParseRequestMissingModel(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"Hello"}]}`
	_, err := parseRequest([]byte(body), http.Header{})
	if err == nil {
		t.Error("parseRequest() should return error for missing model")
	}
}

// --------------------------------------------------------------------------
// writeOpenAIError
// --------------------------------------------------------------------------

func TestWriteOpenAIError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIError(rec, 502, "all providers unavailable")

	if rec.Code != 502 {
		t.Errorf("status = %d, want 502", rec.Code)
	}

	var resp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if resp.Error.Message != "all providers unavailable" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "all providers unavailable")
	}
	if resp.Error.Code != 502 {
		t.Errorf("code = %d, want 502", resp.Error.Code)
	}
}
