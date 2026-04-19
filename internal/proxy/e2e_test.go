package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/fallback"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

// --------------------------------------------------------------------------
// Helpers for E2E tests
// --------------------------------------------------------------------------

func e2eDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// httpMockProvider is a Provider backed by a real httptest.Server.
// Unlike the in-memory mockProvider in handler_test.go, this one makes
// real HTTP calls — exercising the full network stack.
type httpMockProvider struct {
	id     string
	server *httptest.Server
	client *http.Client
}

func (p *httpMockProvider) ID() string      { return p.id }
func (p *httpMockProvider) Name() string    { return p.id }
func (p *httpMockProvider) BaseURL() string { return p.server.URL }
func (p *httpMockProvider) IsAvailable() bool { return true }
func (p *httpMockProvider) SupportsModel(string) bool { return true }

func (p *httpMockProvider) ClassifyError(statusCode int, headers http.Header, body []byte) provider.ErrorClassification {
	return provider.ClassifyOpenAIError(statusCode, headers, body)
}

func (p *httpMockProvider) Send(ctx context.Context, req *provider.ProxyRequest) (*provider.ProxyResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.server.URL+"/v1/chat/completions", strings.NewReader(string(req.RawBody)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-key")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &provider.ProviderError{
			ProviderID: p.id,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return &provider.ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
	}, nil
}

func (p *httpMockProvider) SendStream(ctx context.Context, req *provider.ProxyRequest) (*stream.SSEParser, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.server.URL+"/v1/chat/completions", strings.NewReader(string(req.RawBody)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-key")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &provider.ProviderError{
			ProviderID: p.id,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return stream.NewSSEParser(resp.Body), nil
}

// newE2EHandler creates a proxy Handler wired to the given providers.
// Each provider gets its own circuit breaker. The global chain is built
// in the order providers are passed.
func newE2EHandler(providers ...provider.Provider) *Handler {
	reg := provider.NewRegistry()
	breakers := make(map[string]*circuit.CircuitBreaker)
	var global []fallback.ChainConfig

	for _, p := range providers {
		reg.Register(p)
		breakers[p.ID()] = circuit.New(p.ID(), e2eDiscardLogger())
		global = append(global, fallback.ChainConfig{ProviderID: p.ID(), ModelID: "test-model"})
	}

	selector := fallback.NewChainSelector(global, nil, nil, reg, breakers, e2eDiscardLogger())
	return NewHandler(selector, e2eDiscardLogger())
}

// newE2EHandlerWithBreakers is like newE2EHandler but accepts pre-built
// circuit breakers, so tests can pre-open them.
func newE2EHandlerWithBreakers(breakers map[string]*circuit.CircuitBreaker, providers ...provider.Provider) *Handler {
	reg := provider.NewRegistry()
	var global []fallback.ChainConfig

	for _, p := range providers {
		reg.Register(p)
		global = append(global, fallback.ChainConfig{ProviderID: p.ID(), ModelID: "test-model"})
	}

	selector := fallback.NewChainSelector(global, nil, nil, reg, breakers, e2eDiscardLogger())
	return NewHandler(selector, e2eDiscardLogger())
}

// chatBody returns a minimal valid OpenAI chat completions request body.
func chatBody(model string, stream bool) string {
	return fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Hello"}],"stream":%v}`, model, stream)
}

// openAIErrorBody parses an OpenAI-compatible error from the response body.
type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// --------------------------------------------------------------------------
// E2E Test 1: Happy path — first provider responds OK
// --------------------------------------------------------------------------

func TestE2E_HappyPath_FirstProviderOK(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request has the Authorization header.
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-e2e-1",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "Hello from provider 1"}}},
		})
	}))
	defer backend.Close()

	p1 := &httpMockProvider{id: "provider1", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody("test-model", false)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["id"] != "chatcmpl-e2e-1" {
		t.Errorf("response id = %v, want %q", resp["id"], "chatcmpl-e2e-1")
	}
}

// --------------------------------------------------------------------------
// E2E Test 2: Fallback — first provider fails, second responds
// --------------------------------------------------------------------------

func TestE2E_Fallback_FirstFails_SecondResponds(t *testing.T) {
	// Provider 1: returns 429 rate limit.
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer backend1.Close()

	// Provider 2: returns 200 OK.
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-p2",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "Hello from fallback"}}},
		})
	}))
	defer backend2.Close()

	p1 := &httpMockProvider{id: "provider1", server: backend1, client: backend1.Client()}
	p2 := &httpMockProvider{id: "provider2", server: backend2, client: backend2.Client()}
	handler := newE2EHandler(p1, p2)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody("test-model", false)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback to provider2)", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["id"] != "chatcmpl-from-p2" {
		t.Errorf("response id = %v, want %q (from fallback provider)", resp["id"], "chatcmpl-from-p2")
	}
}

// --------------------------------------------------------------------------
// E2E Test 3: All providers fail
// --------------------------------------------------------------------------

func TestE2E_AllProvidersFail(t *testing.T) {
	makeFailServer := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(`{"error":{"message":"server error"}}`))
		}))
	}

	b1 := makeFailServer(500)
	defer b1.Close()
	b2 := makeFailServer(500)
	defer b2.Close()
	b3 := makeFailServer(500)
	defer b3.Close()

	p1 := &httpMockProvider{id: "p1", server: b1, client: b1.Client()}
	p2 := &httpMockProvider{id: "p2", server: b2, client: b2.Client()}
	p3 := &httpMockProvider{id: "p3", server: b3, client: b3.Client()}
	handler := newE2EHandler(p1, p2, p3)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody("test-model", false)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}

	var errResp openAIErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error.Type != "proxy_error" {
		t.Errorf("error type = %q, want %q", errResp.Error.Type, "proxy_error")
	}
}

// --------------------------------------------------------------------------
// E2E Test 4: Circuit breaker skips open provider
// --------------------------------------------------------------------------

func TestE2E_CircuitBreakerSkipsOpenProvider(t *testing.T) {
	// Provider 1: should never receive a request (circuit open).
	var p1Calls int32
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&p1Calls, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"should-not-reach"}`))
	}))
	defer backend1.Close()

	// Provider 2: responds OK.
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-p2",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "OK"}}},
		})
	}))
	defer backend2.Close()

	p1 := &httpMockProvider{id: "p1", server: backend1, client: backend1.Client()}
	p2 := &httpMockProvider{id: "p2", server: backend2, client: backend2.Client()}

	// Pre-open circuit breaker for p1.
	cbP1 := circuit.New("p1", e2eDiscardLogger())
	for i := 0; i < 3; i++ {
		cbP1.RecordFailure()
	}
	cbP2 := circuit.New("p2", e2eDiscardLogger())

	breakers := map[string]*circuit.CircuitBreaker{"p1": cbP1, "p2": cbP2}
	handler := newE2EHandlerWithBreakers(breakers, p1, p2)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody("test-model", false)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (from p2, skipping p1)", rec.Code)
	}

	// Verify p1 was never called.
	if calls := atomic.LoadInt32(&p1Calls); calls != 0 {
		t.Errorf("p1 received %d calls, want 0 (circuit was open)", calls)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "chatcmpl-from-p2" {
		t.Errorf("response id = %v, want %q", resp["id"], "chatcmpl-from-p2")
	}
}

// --------------------------------------------------------------------------
// E2E Test 5: Invalid request — broken JSON
// --------------------------------------------------------------------------

func TestE2E_InvalidRequest_BrokenJSON(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not receive request for broken JSON")
	}))
	defer backend.Close()

	p1 := &httpMockProvider{id: "p1", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	var errResp openAIErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error.Type != "proxy_error" {
		t.Errorf("error type = %q, want %q", errResp.Error.Type, "proxy_error")
	}
}

// --------------------------------------------------------------------------
// E2E Test 6: Invalid request — missing model
// --------------------------------------------------------------------------

func TestE2E_InvalidRequest_MissingModel(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not receive request when model is missing")
	}))
	defer backend.Close()

	p1 := &httpMockProvider{id: "p1", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)

	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	var errResp openAIErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error.Type != "proxy_error" {
		t.Errorf("error type = %q, want %q", errResp.Error.Type, "proxy_error")
	}
}

// --------------------------------------------------------------------------
// E2E Test 7: Streaming — provider returns SSE events
// --------------------------------------------------------------------------

func TestE2E_Streaming_SSEEvents(t *testing.T) {
	ssePayload := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant","content":""},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-1","choices":[{"delta":{},"index":0,"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer backend.Close()

	p1 := &httpMockProvider{id: "p1", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)

	body := chatBody("test-model", true)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	// Parse the SSE events from the response.
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, line)
		}
	}

	if len(dataLines) < 3 {
		t.Fatalf("got %d data lines, want at least 3 (content chunks + [DONE])", len(dataLines))
	}

	// Verify [DONE] is the last data line.
	lastData := dataLines[len(dataLines)-1]
	if lastData != "data: [DONE]" {
		t.Errorf("last data line = %q, want %q", lastData, "data: [DONE]")
	}

	// Verify content deltas are present.
	allData := rec.Body.String()
	if !strings.Contains(allData, "Hello") {
		t.Error("response should contain 'Hello' content delta")
	}
	if !strings.Contains(allData, " world") {
		t.Error("response should contain ' world' content delta")
	}
}

// --------------------------------------------------------------------------
// E2E Test 8: Transport error — provider unreachable, fallback works
// --------------------------------------------------------------------------

func TestE2E_TransportError_FallbackToSecond(t *testing.T) {
	// Provider 1: uses a closed server to simulate connection refused.
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedServer.Close() // Close immediately — connection refused.

	// Provider 2: responds OK.
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-fallback",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "recovered"}}},
		})
	}))
	defer backend2.Close()

	p1 := &httpMockProvider{id: "p1-unreachable", server: closedServer, client: &http.Client{}}
	p2 := &httpMockProvider{id: "p2-ok", server: backend2, client: backend2.Client()}
	handler := newE2EHandler(p1, p2)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody("test-model", false)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback after transport error)", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "chatcmpl-from-fallback" {
		t.Errorf("response id = %v, want %q", resp["id"], "chatcmpl-from-fallback")
	}
}

// --------------------------------------------------------------------------
// E2E Test 9: Streaming fallback — first provider fails, second streams OK
// --------------------------------------------------------------------------

func TestE2E_StreamingFallback_FirstFails_SecondStreams(t *testing.T) {
	// Provider 1: returns 429 for streaming requests.
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer backend1.Close()

	// Provider 2: streams SSE.
	ssePayload := "data: {\"id\":\"stream-p2\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"index\":0}]}\n\ndata: [DONE]\n\n"
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer backend2.Close()

	p1 := &httpMockProvider{id: "p1", server: backend1, client: backend1.Client()}
	p2 := &httpMockProvider{id: "p2", server: backend2, client: backend2.Client()}
	handler := newE2EHandler(p1, p2)

	body := chatBody("test-model", true)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (streaming fallback)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	respBody := rec.Body.String()
	if !strings.Contains(respBody, "stream-p2") {
		t.Error("response should contain stream from p2")
	}
	if !strings.Contains(respBody, "[DONE]") {
		t.Error("response should contain [DONE]")
	}
}

// --------------------------------------------------------------------------
// E2E Test 10: Multiple fallbacks — first two fail, third succeeds
// --------------------------------------------------------------------------

func TestE2E_MultipleFallbacks_ThirdSucceeds(t *testing.T) {
	// Provider 1: 429 rate limit.
	b1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer b1.Close()

	// Provider 2: 500 server error.
	b2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal error"}}`))
	}))
	defer b2.Close()

	// Provider 3: 200 OK.
	b3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-p3-wins",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "third time's the charm"}}},
		})
	}))
	defer b3.Close()

	p1 := &httpMockProvider{id: "p1", server: b1, client: b1.Client()}
	p2 := &httpMockProvider{id: "p2", server: b2, client: b2.Client()}
	p3 := &httpMockProvider{id: "p3", server: b3, client: b3.Client()}
	handler := newE2EHandler(p1, p2, p3)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody("test-model", false)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (third provider succeeds)", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "chatcmpl-p3-wins" {
		t.Errorf("response id = %v, want %q", resp["id"], "chatcmpl-p3-wins")
	}
}

// --------------------------------------------------------------------------
// E2E Test 11: Request body forwarding integrity
// --------------------------------------------------------------------------

func TestE2E_RequestBodyForwardedCorrectly(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedAuth string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	p1 := &httpMockProvider{id: "p1", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)

	body := `{"model":"test-model","messages":[{"role":"user","content":"detailed request"}],"temperature":0.5}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Verify auth header was sent.
	if receivedAuth != "Bearer test-key" {
		t.Errorf("backend auth = %q, want %q", receivedAuth, "Bearer test-key")
	}

	// Verify the model was in the forwarded body.
	if receivedBody["model"] != "test-model" {
		t.Errorf("forwarded model = %v, want %q", receivedBody["model"], "test-model")
	}

	// Verify temperature was forwarded.
	if temp, ok := receivedBody["temperature"].(float64); !ok || temp != 0.5 {
		t.Errorf("forwarded temperature = %v, want 0.5", receivedBody["temperature"])
	}
}
