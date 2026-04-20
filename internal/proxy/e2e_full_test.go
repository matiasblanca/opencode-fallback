//go:build e2e

package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
)

// --------------------------------------------------------------------------
// Helpers for full E2E tests — these start a real proxy.Server on a random port
// --------------------------------------------------------------------------

// startRealProxy creates a Handler, wraps it in a real Server on 127.0.0.1:0,
// starts it in a goroutine, and returns the base URL (e.g. "http://127.0.0.1:54321").
// The server is shut down when the test finishes via t.Cleanup().
func startRealProxy(t *testing.T, handler http.Handler) string {
	t.Helper()

	// Bind to a random available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Can't t.Fatal in a goroutine; test will fail on assertion instead.
		}
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	return fmt.Sprintf("http://%s", listener.Addr().String())
}

// proxyPost sends a POST /v1/chat/completions to the proxy with the given body.
func proxyPost(t *testing.T, proxyURL, body string, timeout time.Duration) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	return resp
}

// proxyGet sends a GET to the proxy path.
func proxyGet(t *testing.T, proxyURL, path string, timeout time.Duration) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	return resp
}

// readBody reads and closes a response body.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return body
}

// --------------------------------------------------------------------------
// Test 1: Proxy starts and responds to health check
// --------------------------------------------------------------------------

func TestE2E_HealthCheck(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "ok"}}},
		})
	}))
	t.Cleanup(backend.Close)

	p1 := &httpMockProvider{id: "test-provider", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)
	proxyURL := startRealProxy(t, handler)

	resp := proxyGet(t, proxyURL, "/v1/status", 5*time.Second)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}

	if _, ok := status["version"]; !ok {
		t.Error("status response missing 'version' field")
	}
	if _, ok := status["providers"]; !ok {
		t.Error("status response missing 'providers' field")
	}

	// Check the provider appears and is available.
	providers, ok := status["providers"].([]interface{})
	if !ok || len(providers) == 0 {
		t.Fatal("providers array is empty or wrong type")
	}

	found := false
	for _, pRaw := range providers {
		pMap, ok := pRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if pMap["id"] == "test-provider" {
			found = true
			if avail, ok := pMap["available"].(bool); !ok || !avail {
				t.Errorf("provider available = %v, want true", pMap["available"])
			}
			break
		}
	}
	if !found {
		t.Error("provider 'test-provider' not found in status response")
	}
}

// --------------------------------------------------------------------------
// Test 2: Non-streaming request — happy path
// --------------------------------------------------------------------------

func TestE2E_NonStreaming_HappyPath(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-full-e2e",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "Hello from full E2E"}}},
		})
	}))
	t.Cleanup(backend.Close)

	p1 := &httpMockProvider{id: "provider1", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)
	proxyURL := startRealProxy(t, handler)

	resp := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("response missing 'choices' array")
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		t.Fatal("first choice is not an object")
	}
	msg, ok := choice["message"].(map[string]interface{})
	if !ok {
		t.Fatal("choice missing 'message'")
	}
	if msg["content"] != "Hello from full E2E" {
		t.Errorf("content = %q, want %q", msg["content"], "Hello from full E2E")
	}
}

// --------------------------------------------------------------------------
// Test 3: Streaming request — happy path
// --------------------------------------------------------------------------

func TestE2E_Streaming_HappyPath(t *testing.T) {
	ssePayload := strings.Join([]string{
		`data: {"id":"chatcmpl-stream","choices":[{"delta":{"role":"assistant","content":""},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-stream","choices":[{"delta":{"content":"Hello"},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-stream","choices":[{"delta":{"content":" E2E"},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-stream","choices":[{"delta":{"content":" world"},"index":0}]}`,
		"",
		`data: {"id":"chatcmpl-stream","choices":[{"delta":{},"index":0,"finish_reason":"stop"}]}`,
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
	t.Cleanup(backend.Close)

	p1 := &httpMockProvider{id: "stream-provider", server: backend, client: backend.Client()}
	handler := newE2EHandler(p1)
	proxyURL := startRealProxy(t, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL+"/v1/chat/completions", strings.NewReader(chatBody("test-model", true)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Parse SSE events.
	scanner := bufio.NewScanner(resp.Body)
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, line)
		}
	}

	if len(dataLines) < 3 {
		t.Fatalf("got %d data lines, want at least 3", len(dataLines))
	}

	// Verify [DONE] is last.
	lastData := dataLines[len(dataLines)-1]
	if lastData != "data: [DONE]" {
		t.Errorf("last data line = %q, want %q", lastData, "data: [DONE]")
	}

	// Assemble delta text.
	var assembled string
	for _, line := range dataLines {
		raw := strings.TrimPrefix(line, "data: ")
		if raw == "[DONE]" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		choices, ok := ev["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		ch, ok := choices[0].(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := ch["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := delta["content"].(string); ok {
			assembled += content
		}
	}

	if assembled != "Hello E2E world" {
		t.Errorf("assembled text = %q, want %q", assembled, "Hello E2E world")
	}
}

// --------------------------------------------------------------------------
// Test 4: Fallback — first provider fails (500), second succeeds
// --------------------------------------------------------------------------

func TestE2E_Fallback_ServerError(t *testing.T) {
	var backendACalls int32
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendACalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal server error","type":"server_error"}}`))
	}))
	t.Cleanup(backendA.Close)

	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-B",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "Response from B"}}},
		})
	}))
	t.Cleanup(backendB.Close)

	pA := &httpMockProvider{id: "providerA", server: backendA, client: backendA.Client()}
	pB := &httpMockProvider{id: "providerB", server: backendB, client: backendB.Client()}
	handler := newE2EHandler(pA, pB)
	proxyURL := startRealProxy(t, handler)

	resp := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback to B); body: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["id"] != "chatcmpl-from-B" {
		t.Errorf("response id = %v, want %q", result["id"], "chatcmpl-from-B")
	}

	// Backend A should have been tried (original + retry = 2 calls with default maxRetries=1).
	if calls := atomic.LoadInt32(&backendACalls); calls == 0 {
		t.Error("backend A should have received at least one request")
	}
}

// --------------------------------------------------------------------------
// Test 5: Fallback — connection refused triggers fallback
// --------------------------------------------------------------------------

func TestE2E_Fallback_ConnectionRefused(t *testing.T) {
	// Provider A: bind a port and immediately close it = connection refused.
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedServer.Close()

	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-conn-refused-fallback",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "recovered after conn refused"}}},
		})
	}))
	t.Cleanup(backendB.Close)

	pA := &httpMockProvider{id: "provider-dead", server: closedServer, client: &http.Client{}}
	pB := &httpMockProvider{id: "provider-alive", server: backendB, client: backendB.Client()}
	handler := newE2EHandler(pA, pB)
	proxyURL := startRealProxy(t, handler)

	resp := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback after conn refused); body: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["id"] != "chatcmpl-from-conn-refused-fallback" {
		t.Errorf("response id = %v, want %q", result["id"], "chatcmpl-from-conn-refused-fallback")
	}
}

// --------------------------------------------------------------------------
// Test 6: Circuit breaker trips after threshold failures
// --------------------------------------------------------------------------

func TestE2E_CircuitBreaker_Trips(t *testing.T) {
	var backendCalls int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"server error","type":"server_error"}}`))
	}))
	t.Cleanup(backend.Close)

	p1 := &httpMockProvider{id: "failing-provider", server: backend, client: backend.Client()}

	// Use custom circuit breaker with threshold=3 and no retry (maxRetries=0 equivalent
	// is achieved by using the default which is 1 retry — but we want exactly threshold
	// hits). The default CB threshold is 3 and server_error has weight 2,
	// so 2 requests × 2 weight = 4 ≥ 3, circuit opens after 2 request attempts.
	// But with retry, each request attempt = 2 calls (original + 1 retry).
	// Let's use custom breaker with threshold=3.
	cb := circuit.New("failing-provider", e2eDiscardLogger())
	breakers := map[string]*circuit.CircuitBreaker{"failing-provider": cb}
	handler := newE2EHandlerWithBreakers(breakers, p1)
	proxyURL := startRealProxy(t, handler)

	// Send requests until circuit opens.
	// server_error weight = 2, threshold = 3, so after 2 failed provider attempts
	// (which means 2 proxy requests, since each fails with server_error on original+retry),
	// the circuit should open.
	// Actually, let's just send 4 requests and check behavior.
	var responses []*http.Response
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			proxyURL+"/v1/chat/completions",
			strings.NewReader(chatBody("test-model", false)))
		if err != nil {
			cancel()
			t.Fatalf("build request %d: %v", i, err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		responses = append(responses, resp)
	}

	// All responses should be errors (502 bad gateway = all providers failed).
	for i, resp := range responses {
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("request %d: status = %d, want 502", i, resp.StatusCode)
		}
	}

	// The circuit breaker should now be open. Backend should have received
	// fewer calls for the 4th request than for the 1st (because circuit blocked it).
	// With threshold=3, weight=2 per server_error, after the 1st proxy request:
	// - original attempt → server_error (weight 2 recorded to CB: count=2)
	// - retry → server_error (count still 2 because only lastFailure is recorded once)
	// Wait — looking at the chain code: only lastFailure is recorded ONCE after all
	// retries are exhausted. So per proxy request: 1 RecordFailureWithReason call.
	// server_error weight = 2, so:
	// - After request 1: count=2
	// - After request 2: count=4 ≥ 3 → circuit opens
	// - Request 3: circuit is open → 0 backend calls → "circuit_open" failure → 502
	// - Request 4: same

	callsAfter := atomic.LoadInt32(&backendCalls)

	// The backend should have been called for requests 1 and 2 (with retries),
	// but NOT for requests 3 and 4 (circuit open).
	// Requests 1-2: 2 attempts each (original + 1 retry) = 4 backend calls.
	// Requests 3-4: 0 backend calls (circuit open).
	// Total expected: 4 calls.
	if callsAfter > 6 {
		t.Errorf("backend received %d calls; expected ≤6 (circuit should have blocked later requests)", callsAfter)
	}

	// Verify the circuit breaker is open.
	state := cb.CurrentState()
	if state != circuit.StateOpen {
		t.Errorf("circuit state = %v, want Open", state)
	}
}

// --------------------------------------------------------------------------
// Test 7: Rate limit with Retry-After
// --------------------------------------------------------------------------

func TestE2E_RateLimit_RetryAfter(t *testing.T) {
	var backendACalls int32
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendACalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	t.Cleanup(backendA.Close)

	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-B-ratelimit",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "from B"}}},
		})
	}))
	t.Cleanup(backendB.Close)

	pA := &httpMockProvider{id: "provider-ratelimited", server: backendA, client: backendA.Client()}
	pB := &httpMockProvider{id: "provider-healthy", server: backendB, client: backendB.Client()}
	handler := newE2EHandler(pA, pB)
	proxyURL := startRealProxy(t, handler)

	// First request: A fails with 429, retries after backoff (capped at 10s), then falls back to B.
	// Needs generous timeout to accommodate the retry delay.
	resp1 := proxyPost(t, proxyURL, chatBody("test-model", false), 15*time.Second)
	body1 := readBody(t, resp1)

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("request 1: status = %d, want 200; body: %s", resp1.StatusCode, body1)
	}
	var result1 map[string]interface{}
	json.Unmarshal(body1, &result1)
	if result1["id"] != "chatcmpl-from-B-ratelimit" {
		t.Errorf("request 1: id = %v, want from B", result1["id"])
	}

	callsAfterFirst := atomic.LoadInt32(&backendACalls)

	// Second request immediately — A should be in cooldown.
	resp2 := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body2 := readBody(t, resp2)

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("request 2: status = %d, want 200; body: %s", resp2.StatusCode, body2)
	}

	callsAfterSecond := atomic.LoadInt32(&backendACalls)

	// A should NOT have been called for the second request (in cooldown).
	if callsAfterSecond > callsAfterFirst {
		t.Errorf("backend A received %d more calls after cooldown; expected 0 (cooldown should block)",
			callsAfterSecond-callsAfterFirst)
	}
}

// --------------------------------------------------------------------------
// Test 8: Overflow error does NOT trigger fallback
// --------------------------------------------------------------------------

func TestE2E_Overflow_BlocksFallback(t *testing.T) {
	var backupCalls int32

	backendOverflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"prompt is too long","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(backendOverflow.Close)

	backendBackup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-should-not-reach",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "wrong"}}},
		})
	}))
	t.Cleanup(backendBackup.Close)

	pOverflow := &httpMockProvider{id: "overflow-provider", server: backendOverflow, client: backendOverflow.Client()}
	pBackup := &httpMockProvider{id: "backup-provider", server: backendBackup, client: backendBackup.Client()}
	handler := newE2EHandler(pOverflow, pBackup)
	proxyURL := startRealProxy(t, handler)

	resp := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body := readBody(t, resp)

	// The proxy should return an error (overflow is fatal, no fallback).
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("status = 200, want an error status (overflow should not fallback); body: %s", body)
	}

	// The backup provider should NOT have been called.
	if calls := atomic.LoadInt32(&backupCalls); calls != 0 {
		t.Errorf("backup provider received %d calls, want 0 (overflow blocks fallback)", calls)
	}
}

// --------------------------------------------------------------------------
// Test 9: Streaming fallback — first provider fails, second streams OK
// --------------------------------------------------------------------------

func TestE2E_StreamingFallback(t *testing.T) {
	var backendACalls int32
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendACalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"service unavailable","type":"server_error"}}`))
	}))
	t.Cleanup(backendA.Close)

	ssePayload := strings.Join([]string{
		`data: {"id":"stream-from-B","choices":[{"delta":{"content":"streamed"},"index":0}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	t.Cleanup(backendB.Close)

	pA := &httpMockProvider{id: "stream-fail", server: backendA, client: backendA.Client()}
	pB := &httpMockProvider{id: "stream-ok", server: backendB, client: backendB.Client()}
	handler := newE2EHandler(pA, pB)
	proxyURL := startRealProxy(t, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL+"/v1/chat/completions",
		strings.NewReader(chatBody("test-model", true)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "stream-from-B") {
		t.Error("response should contain stream from provider B")
	}
	if !strings.Contains(string(respBody), "[DONE]") {
		t.Error("response should contain [DONE]")
	}

	// Backend A should have been tried first.
	if calls := atomic.LoadInt32(&backendACalls); calls == 0 {
		t.Error("backend A should have been called at least once")
	}
}

// --------------------------------------------------------------------------
// Test 10: Health scoring reorders providers
// --------------------------------------------------------------------------

func TestE2E_HealthScoring_Reorder(t *testing.T) {
	var backendACalls, backendBCalls, backendCCalls int32

	makeBackend := func(id string, counter *int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(counter, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      fmt.Sprintf("chatcmpl-from-%s", id),
				"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "from " + id}}},
			})
		}))
	}

	bA := makeBackend("A", &backendACalls)
	t.Cleanup(bA.Close)
	bB := makeBackend("B", &backendBCalls)
	t.Cleanup(bB.Close)
	bC := makeBackend("C", &backendCCalls)
	t.Cleanup(bC.Close)

	pA := &httpMockProvider{id: "provA", server: bA, client: bA.Client()}
	pB := &httpMockProvider{id: "provB", server: bB, client: bB.Client()}
	pC := &httpMockProvider{id: "provC", server: bC, client: bC.Client()}

	// Create breakers: A = open (failed), B = closed (healthy), C = open with cooldown (rate limited).
	cbA := circuit.New("provA", e2eDiscardLogger())
	// Trip A's circuit: 3 failures to open it.
	for i := 0; i < 3; i++ {
		cbA.RecordFailure()
	}

	cbB := circuit.New("provB", e2eDiscardLogger())
	// B stays closed (healthy).

	cbC := circuit.New("provC", e2eDiscardLogger())
	// Trip C with rate limit cooldown.
	cbC.RecordRateLimitWithCooldown(60 * time.Second)

	breakers := map[string]*circuit.CircuitBreaker{
		"provA": cbA,
		"provB": cbB,
		"provC": cbC,
	}

	handler := newE2EHandlerWithBreakers(breakers, pA, pB, pC)
	proxyURL := startRealProxy(t, handler)

	resp := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	// B should respond (highest health score = 3, vs A=0, C=1).
	if result["id"] != "chatcmpl-from-B" {
		t.Errorf("response id = %v, want 'chatcmpl-from-B' (B has highest health score)", result["id"])
	}

	// B should have been called, A and C should not.
	if atomic.LoadInt32(&backendBCalls) == 0 {
		t.Error("backend B should have been called (highest health score)")
	}
	if atomic.LoadInt32(&backendACalls) != 0 {
		t.Error("backend A should NOT have been called (circuit open)")
	}
	// C is open with cooldown; Allow() should return false.
	if atomic.LoadInt32(&backendCCalls) != 0 {
		t.Error("backend C should NOT have been called (circuit open with cooldown)")
	}
}

// --------------------------------------------------------------------------
// Test 11: Context cancellation stops the chain (abort safety)
// --------------------------------------------------------------------------

func TestE2E_AbortSafety(t *testing.T) {
	// Backend that takes 5 seconds to respond.
	slowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"slow","choices":[{"message":{"content":"late"}}]}`))
		case <-r.Context().Done():
			// Client disconnected.
			return
		}
	}))
	t.Cleanup(slowBackend.Close)

	backendBackup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"backup","choices":[{"message":{"content":"backup"}}]}`))
	}))
	t.Cleanup(backendBackup.Close)

	p1 := &httpMockProvider{id: "slow-provider", server: slowBackend, client: slowBackend.Client()}
	p2 := &httpMockProvider{id: "backup-provider", server: backendBackup, client: backendBackup.Client()}

	cb := circuit.New("slow-provider", e2eDiscardLogger())
	cbBackup := circuit.New("backup-provider", e2eDiscardLogger())
	breakers := map[string]*circuit.CircuitBreaker{
		"slow-provider":   cb,
		"backup-provider": cbBackup,
	}
	handler := newE2EHandlerWithBreakers(breakers, p1, p2)
	proxyURL := startRealProxy(t, handler)

	// Send a request with a 500ms timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL+"/v1/chat/completions",
		strings.NewReader(chatBody("test-model", false)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	_, err = http.DefaultClient.Do(req)
	// We expect the client to get an error (context deadline exceeded) OR
	// the proxy to return an error response before we read the body.
	if err == nil {
		// If we got a response, it should be an error status.
		t.Log("request did not error out on client side (proxy responded before timeout)")
	}

	// Wait a moment for the proxy to process.
	time.Sleep(200 * time.Millisecond)

	// Circuit breaker should still be Closed — abort doesn't penalize.
	state := cb.CurrentState()
	if state != circuit.StateClosed {
		t.Errorf("circuit state = %v, want Closed (abort should not penalize provider)", state)
	}
}

// --------------------------------------------------------------------------
// Test 12: Quota exhaustion — falls through to next provider
// --------------------------------------------------------------------------

func TestE2E_QuotaExhausted_FallsThrough(t *testing.T) {
	var backendACalls int32
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendACalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"You exceeded your current quota","type":"insufficient_quota"}}`))
	}))
	t.Cleanup(backendA.Close)

	var backendBCalls int32
	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendBCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-from-quota-fallback",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "from backup after quota"}}},
		})
	}))
	t.Cleanup(backendB.Close)

	pA := &httpMockProvider{id: "quota-exhausted", server: backendA, client: backendA.Client()}
	pB := &httpMockProvider{id: "healthy-backup", server: backendB, client: backendB.Client()}
	handler := newE2EHandler(pA, pB)
	proxyURL := startRealProxy(t, handler)

	resp := proxyPost(t, proxyURL, chatBody("test-model", false), 5*time.Second)
	body := readBody(t, resp)

	// quota_exhausted is fatal for retry-same-provider, but falls through
	// to the next provider in the chain.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (should fall through to backup); body: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)
	if result["id"] != "chatcmpl-from-quota-fallback" {
		t.Errorf("response id = %v, want %q", result["id"], "chatcmpl-from-quota-fallback")
	}

	// A should have been tried once (no retry since quota_exhausted is fatal for retry).
	if calls := atomic.LoadInt32(&backendACalls); calls != 1 {
		t.Errorf("backend A received %d calls, want 1 (no retry on quota exhaustion)", calls)
	}

	// B should have been called (fallback).
	if calls := atomic.LoadInt32(&backendBCalls); calls != 1 {
		t.Errorf("backend B received %d calls, want 1 (should receive fallback)", calls)
	}
}
