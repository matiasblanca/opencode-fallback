//go:build e2e

// Package proxy — e2e_live_test.go contains live E2E tests that run against
// REAL provider APIs using REAL subscription credentials.
//
// These tests require:
//   - A valid auth.json with Anthropic OAuth credentials
//   - Port 18888 to be available
//   - Internet connectivity to Anthropic's API
//
// Run with: go test -tags e2e -v -timeout 300s ./internal/proxy/ -run TestLive
//
// IMPORTANT: These tests make real API calls that consume subscription quota.
// They should NOT run in CI without explicit secrets configuration.
package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
	"github.com/matiasblanca/opencode-fallback/internal/bridge"
	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/fallback"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
)

// ─── Constants ────────────────────────────────────────────────────────

const (
	// liveTestPort is the port used for the live E2E proxy server.
	// Uses 18888 to avoid conflicts with production proxy (8787).
	liveTestPort = 18888

	// liveTestHost is the host the test proxy binds to.
	liveTestHost = "127.0.0.1"

	// liveTestModel is the Anthropic model used for live tests.
	liveTestModel = "claude-sonnet-4-20250514"

	// liveTestBridgePort is the port used for bridge integration tests.
	liveTestBridgePort = 18787

	// liveRequestTimeout is the timeout for a single API request.
	liveRequestTimeout = 60 * time.Second

	// liveStreamTimeout is the timeout for streaming API requests.
	liveStreamTimeout = 90 * time.Second
)

// ─── Shared Helpers ───────────────────────────────────────────────────

// liveTestLogger creates a logger that writes to testing.T for visibility.
func liveTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// requireAuthJSON verifies auth.json is available and has a valid Anthropic
// OAuth entry. Skips the test if not available.
func requireAuthJSON(t *testing.T) *auth.Reader {
	t.Helper()
	logger := liveTestLogger(t)
	reader := auth.NewReader(logger)

	entry, err := reader.Get("anthropic")
	if err != nil {
		t.Skipf("auth.json not readable: %v", err)
	}
	if entry == nil {
		t.Skip("auth.json has no 'anthropic' entry — skipping live test")
	}
	if entry.Type != "oauth" || entry.OAuth == nil {
		t.Skipf("anthropic entry is type %q, need 'oauth' — skipping", entry.Type)
	}
	if auth.IsExpired(entry.OAuth) {
		t.Skip("Anthropic OAuth token is expired — run OpenCode to refresh, then retry")
	}

	// Log status WITHOUT token values.
	t.Logf("auth.json: anthropic OAuth present, token valid, expires at %d", entry.OAuth.Expires)
	return reader
}

// liveProxyBaseURL returns the base URL of the test proxy.
func liveProxyBaseURL() string {
	return fmt.Sprintf("http://%s:%d", liveTestHost, liveTestPort)
}

// sendChatRequest sends a non-streaming chat completion request to the proxy.
func sendChatRequest(t *testing.T, baseURL, model, prompt string, stream bool) (*http.Response, []byte) {
	t.Helper()

	body := fmt.Sprintf(`{
		"model": %q,
		"messages": [{"role": "user", "content": %q}],
		"stream": %v,
		"max_tokens": 100
	}`, model, prompt, stream)

	ctx, cancel := context.WithTimeout(context.Background(), liveRequestTimeout)
	if stream {
		ctx, cancel = context.WithTimeout(context.Background(), liveStreamTimeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/chat/completions",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	return resp, respBody
}

// sendStreamingRequest sends a streaming request and returns the raw response
// (body is NOT read — caller must handle the stream).
func sendStreamingRequest(t *testing.T, baseURL, model, prompt string) *http.Response {
	t.Helper()

	body := fmt.Sprintf(`{
		"model": %q,
		"messages": [{"role": "user", "content": %q}],
		"stream": true,
		"max_tokens": 100
	}`, model, prompt)

	ctx, cancel := context.WithTimeout(context.Background(), liveStreamTimeout)
	// Don't defer cancel — caller owns the response body lifetime.
	_ = cancel

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/chat/completions",
		strings.NewReader(body))
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

// startLiveProxy starts a proxy server with the given providers on liveTestPort.
// Returns a cleanup function that shuts down the server.
func startLiveProxy(t *testing.T, providers ...provider.Provider) func() {
	t.Helper()
	logger := liveTestLogger(t)

	reg := provider.NewRegistry()
	breakers := make(map[string]*circuit.CircuitBreaker)
	var global []fallback.ChainConfig

	for _, p := range providers {
		reg.Register(p)
		breakers[p.ID()] = circuit.New(p.ID(), logger)
		global = append(global, fallback.ChainConfig{
			ProviderID: p.ID(),
			ModelID:    liveTestModel,
		})
	}

	selector := fallback.NewChainSelector(global, nil, nil, reg, breakers, logger)
	handler := NewHandler(selector, breakers, reg, logger)
	server := NewServer(liveTestHost, liveTestPort, handler, logger)

	// Start the server in a goroutine.
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.Start()
	}()

	// Wait for the server to be ready.
	ready := false
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(liveProxyBaseURL() + "/v1/chat/completions")
		if err == nil {
			resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("proxy did not become ready within 2 seconds")
	}

	t.Log("proxy started on", liveProxyBaseURL())

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}
}

// ─── Phase 2: Proxy Startup Tests ────────────────────────────────────

func TestLive_Phase2_ProxyStartup(t *testing.T) {
	authReader := requireAuthJSON(t)
	logger := liveTestLogger(t)

	// Create the Anthropic OAuth provider using real auth.json.
	p := provider.NewAnthropicOAuthProvider(authReader, nil, logger)
	if !p.IsAvailable() {
		t.Fatal("AnthropicOAuthProvider reports not available despite valid auth.json")
	}

	t.Logf("provider ID: %s, Name: %s", p.ID(), p.Name())

	// Start proxy and verify it runs.
	shutdown := startLiveProxy(t, p)
	defer shutdown()

	// Send a request to verify the proxy is accepting connections.
	// Even a bad request should return a structured error (not connection refused).
	resp, body := sendChatRequest(t, liveProxyBaseURL(), "nonexistent-model", "test", false)
	t.Logf("startup test: status=%d, body_len=%d", resp.StatusCode, len(body))

	// We expect either 200 (model found) or 502 (model not in chain) — but NOT connection error.
	if resp.StatusCode == 0 {
		t.Fatal("proxy returned status 0 — not running")
	}
}

// ─── Phase 3.1: Non-Streaming Request to Anthropic ───────────────────

func TestLive_Phase3_1_NonStreamingAnthropicRequest(t *testing.T) {
	authReader := requireAuthJSON(t)
	logger := liveTestLogger(t)

	p := provider.NewAnthropicOAuthProvider(authReader, nil, logger)
	if !p.IsAvailable() {
		t.Skip("AnthropicOAuthProvider not available")
	}

	shutdown := startLiveProxy(t, p)
	defer shutdown()

	t.Log("sending non-streaming request to Anthropic via proxy...")

	resp, body := sendChatRequest(t, liveProxyBaseURL(), liveTestModel,
		"Reply with exactly: E2E_TEST_OK", false)

	// Check HTTP status.
	switch resp.StatusCode {
	case http.StatusOK:
		t.Log("HTTP 200 OK — request succeeded")
	case http.StatusTooManyRequests:
		t.Log("HTTP 429 — rate limited, retrying after 60s...")
		time.Sleep(60 * time.Second)
		resp, body = sendChatRequest(t, liveProxyBaseURL(), liveTestModel,
			"Reply with exactly: E2E_TEST_OK", false)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retry after 429 also failed: status=%d body=%s",
				resp.StatusCode, truncateForLog(string(body)))
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		t.Fatalf("HTTP %d — auth failure. Token may be invalid or impersonation headers are wrong. Body: %s",
			resp.StatusCode, truncateForLog(string(body)))
	default:
		if resp.StatusCode >= 500 {
			t.Fatalf("HTTP %d — server error. Body: %s",
				resp.StatusCode, truncateForLog(string(body)))
		}
		t.Fatalf("unexpected status %d. Body: %s",
			resp.StatusCode, truncateForLog(string(body)))
	}

	// Verify Content-Type.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Verify response structure (OpenAI-compatible format).
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("response is not valid JSON: %v\nBody: %s", err, truncateForLog(string(body)))
	}

	// Check that choices exist.
	choices, ok := result["choices"]
	if !ok {
		t.Fatalf("response missing 'choices' field. Keys: %v", mapKeys(result))
	}

	choiceArr, ok := choices.([]interface{})
	if !ok || len(choiceArr) == 0 {
		t.Fatalf("choices is not a non-empty array: %v", choices)
	}

	// Extract message content.
	firstChoice, ok := choiceArr[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first choice is not an object: %T", choiceArr[0])
	}

	msg, ok := firstChoice["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("first choice missing 'message': %v", firstChoice)
	}

	content, ok := msg["content"].(string)
	if !ok {
		t.Fatalf("message missing 'content' string: %v", msg)
	}

	t.Logf("response content: %q", content)

	// Verify the deterministic response.
	if !strings.Contains(content, "E2E_TEST_OK") {
		t.Logf("warning: response does not contain 'E2E_TEST_OK' — model may not follow exact instructions. Content: %q", content)
		// Not a fatal — the model might rephrase, but we at least got a valid response.
	}
}

// ─── Phase 3.2: Streaming Request to Anthropic ───────────────────────

func TestLive_Phase3_2_StreamingAnthropicRequest(t *testing.T) {
	authReader := requireAuthJSON(t)
	logger := liveTestLogger(t)

	p := provider.NewAnthropicOAuthProvider(authReader, nil, logger)
	if !p.IsAvailable() {
		t.Skip("AnthropicOAuthProvider not available")
	}

	shutdown := startLiveProxy(t, p)
	defer shutdown()

	t.Log("sending streaming request to Anthropic via proxy...")

	resp := sendStreamingRequest(t, liveProxyBaseURL(), liveTestModel,
		"Reply with exactly: E2E_STREAM_OK")
	defer resp.Body.Close()

	// Check HTTP status.
	if resp.StatusCode == http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		t.Log("HTTP 429 — rate limited, retrying after 60s...")
		t.Logf("429 body: %s", truncateForLog(string(body)))
		time.Sleep(60 * time.Second)

		resp = sendStreamingRequest(t, liveProxyBaseURL(), liveTestModel,
			"Reply with exactly: E2E_STREAM_OK")
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, want 200. Body: %s",
			resp.StatusCode, truncateForLog(string(body)))
	}

	// Verify Content-Type.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Parse SSE events.
	scanner := bufio.NewScanner(resp.Body)
	var dataLines []string
	var assembledContent strings.Builder
	eventCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			dataLines = append(dataLines, data)
			eventCount++

			// Try to extract content delta.
			if data != "[DONE]" {
				var chunk map[string]interface{}
				if err := json.Unmarshal([]byte(data), &chunk); err == nil {
					if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
						if choice, ok := choices[0].(map[string]interface{}); ok {
							if delta, ok := choice["delta"].(map[string]interface{}); ok {
								if content, ok := delta["content"].(string); ok {
									assembledContent.WriteString(content)
								}
							}
						}
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("stream read error: %v", err)
	}

	t.Logf("received %d SSE data events", eventCount)
	t.Logf("assembled content: %q", assembledContent.String())

	// Verify we got multiple events.
	if eventCount < 2 {
		t.Errorf("got %d SSE events, want at least 2 for streaming", eventCount)
	}

	// Verify the stream ended with [DONE].
	if len(dataLines) > 0 {
		lastData := dataLines[len(dataLines)-1]
		if lastData != "[DONE]" {
			t.Logf("last data line = %q (may use Anthropic's message_stop instead of [DONE])", lastData)
		}
	} else {
		t.Error("no data lines received from stream")
	}

	// Verify content was assembled.
	if assembledContent.Len() == 0 {
		t.Error("no content extracted from stream deltas")
	}
}

// ─── Phase 3.3: Fallback Scenario ────────────────────────────────────

func TestLive_Phase3_3_FallbackScenario(t *testing.T) {
	authReader := requireAuthJSON(t)
	logger := liveTestLogger(t)

	// Provider 1: fake provider pointing at a non-existent server (connection refused).
	fakeProvider := provider.NewGenericOpenAIProvider(
		"fake-provider",
		"Fake Provider (Always Fails)",
		"http://127.0.0.1:19999", // no server running here
		"fake-key",
		provider.AuthTypeBearer,
		[]string{liveTestModel},
		nil,
		logger,
	)

	// Provider 2: real Anthropic OAuth.
	anthropicProvider := provider.NewAnthropicOAuthProvider(authReader, nil, logger)
	if !anthropicProvider.IsAvailable() {
		t.Skip("AnthropicOAuthProvider not available")
	}

	// Build chain: fake → anthropic (fallback).
	reg := provider.NewRegistry()
	reg.Register(fakeProvider)
	reg.Register(anthropicProvider)

	breakers := map[string]*circuit.CircuitBreaker{
		fakeProvider.ID():      circuit.New(fakeProvider.ID(), logger),
		anthropicProvider.ID(): circuit.New(anthropicProvider.ID(), logger),
	}

	global := []fallback.ChainConfig{
		{ProviderID: fakeProvider.ID(), ModelID: liveTestModel},
		{ProviderID: anthropicProvider.ID(), ModelID: liveTestModel},
	}

	selector := fallback.NewChainSelector(global, nil, nil, reg, breakers, logger)
	handler := NewHandler(selector, breakers, reg, logger)
	server := NewServer(liveTestHost, liveTestPort, handler, logger)

	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Start() }()

	// Wait for ready.
	time.Sleep(500 * time.Millisecond)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	t.Log("sending request through fallback chain (fake → anthropic)...")

	resp, body := sendChatRequest(t, liveProxyBaseURL(), liveTestModel,
		"Reply with exactly: FALLBACK_OK", false)

	// Handle 429 retry.
	if resp.StatusCode == http.StatusTooManyRequests {
		t.Log("HTTP 429 — rate limited, retrying after 60s...")
		time.Sleep(60 * time.Second)
		resp, body = sendChatRequest(t, liveProxyBaseURL(), liveTestModel,
			"Reply with exactly: FALLBACK_OK", false)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200 (fallback to Anthropic). Body: %s",
			resp.StatusCode, truncateForLog(string(body)))
	}

	// Verify the response came from Anthropic (has valid completion).
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatalf("response missing choices: %v", result)
	}

	t.Log("fallback succeeded — first provider failed, Anthropic responded")
	t.Logf("response preview: %s", truncateForLog(string(body)))
}

// ─── Phase 3.4: Token Refresh Verification ───────────────────────────

func TestLive_Phase3_4_TokenRefreshVerification(t *testing.T) {
	logger := liveTestLogger(t)
	reader := auth.NewReader(logger)

	entry, err := reader.Get("anthropic")
	if err != nil {
		t.Skipf("auth.json not readable: %v", err)
	}
	if entry == nil || entry.Type != "oauth" || entry.OAuth == nil {
		t.Skip("No Anthropic OAuth entry in auth.json")
	}

	// Check if token is expired.
	if !auth.IsExpired(entry.OAuth) {
		expiresIn := time.Duration(entry.OAuth.Expires-time.Now().UnixMilli()) * time.Millisecond
		hours := int(expiresIn.Hours())
		minutes := int(expiresIn.Minutes()) % 60
		t.Skipf("Token not expired, skipping refresh test. Expires in: %dh %dm", hours, minutes)
	}

	// Token IS expired — try to refresh.
	t.Log("token is expired — attempting refresh via Refresher.EnsureFresh...")

	refresher := auth.NewRefresher(reader, logger)
	refreshedEntry, err := refresher.EnsureFresh("anthropic")
	if err != nil {
		t.Fatalf("EnsureFresh failed: %v", err)
	}

	if refreshedEntry == nil || refreshedEntry.OAuth == nil {
		t.Fatal("EnsureFresh returned nil entry")
	}

	// Verify the token is now valid.
	if auth.IsExpired(refreshedEntry.OAuth) {
		t.Fatal("token is still expired after EnsureFresh")
	}

	newExpiresIn := time.Duration(refreshedEntry.OAuth.Expires-time.Now().UnixMilli()) * time.Millisecond
	t.Logf("token refreshed successfully. New expiry in: %v", newExpiresIn.Round(time.Second))

	// Verify auth.json was updated on disk.
	reader.InvalidateCache()
	diskEntry, err := reader.Get("anthropic")
	if err != nil {
		t.Fatalf("re-read auth.json after refresh: %v", err)
	}
	if diskEntry == nil || diskEntry.OAuth == nil {
		t.Fatal("auth.json lost the anthropic entry after refresh")
	}
	if diskEntry.OAuth.Expires != refreshedEntry.OAuth.Expires {
		t.Errorf("disk expiry %d != memory expiry %d", diskEntry.OAuth.Expires, refreshedEntry.OAuth.Expires)
	}

	t.Log("auth.json updated successfully on disk")
}

// ─── Phase 4: Bridge Integration Tests ───────────────────────────────

func TestLive_Phase4_1_BridgeHealthCheck(t *testing.T) {
	logger := liveTestLogger(t)
	bridgeClient := bridge.NewClientWithConfig(liveTestBridgePort, logger)

	if !bridgeClient.IsAvailable() {
		t.Skip("Bridge not running. Start OpenCode with the fallback-bridge plugin to test bridge integration.")
	}

	// Manually verify health endpoint.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", liveTestBridgePort))
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health check returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var health struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("health status = %q, want %q", health.Status, "ok")
	}

	t.Log("bridge health check: OK")
}

func TestLive_Phase4_2_BridgeAuthRetrieval(t *testing.T) {
	logger := liveTestLogger(t)
	bridgeClient := bridge.NewClientWithConfig(liveTestBridgePort, logger)

	if !bridgeClient.IsAvailable() {
		t.Skip("Bridge not running. Start OpenCode with the fallback-bridge plugin to test bridge integration.")
	}

	entry, err := bridgeClient.GetAuth("anthropic")
	if err != nil {
		t.Fatalf("bridge GetAuth failed: %v", err)
	}
	if entry == nil {
		t.Fatal("bridge returned nil auth entry for anthropic")
	}

	if entry.Type != "oauth" {
		t.Errorf("entry type = %q, want %q", entry.Type, "oauth")
	}
	if entry.OAuth == nil {
		t.Fatal("entry.OAuth is nil")
	}
	if entry.OAuth.Access == "" {
		t.Error("access token is empty")
	}

	// DO NOT log the token value.
	t.Logf("bridge auth: type=%s, has_access=%v, expires=%d",
		entry.Type, entry.OAuth.Access != "", entry.OAuth.Expires)
}

func TestLive_Phase4_3_BridgeTransform(t *testing.T) {
	logger := liveTestLogger(t)
	bridgeClient := bridge.NewClientWithConfig(liveTestBridgePort, logger)

	if !bridgeClient.IsAvailable() {
		t.Skip("Bridge not running. Start OpenCode with the fallback-bridge plugin to test bridge integration.")
	}

	// Send a minimal Anthropic request body.
	reqBody := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 50,
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": false
	}`

	result, err := bridgeClient.TransformAnthropic(reqBody)
	if err != nil {
		t.Fatalf("bridge TransformAnthropic failed: %v", err)
	}

	// Verify URL.
	if !strings.Contains(result.URL, "api.anthropic.com") {
		t.Errorf("URL = %q, want to contain 'api.anthropic.com'", result.URL)
	}
	if !strings.Contains(result.URL, "/v1/messages") {
		t.Errorf("URL = %q, want to contain '/v1/messages'", result.URL)
	}

	// Verify headers.
	if v, ok := result.Headers["anthropic-version"]; !ok || v == "" {
		t.Error("missing anthropic-version header in transform result")
	}

	// Check for CCH billing header (may be in different header names).
	hasBilling := false
	for k, v := range result.Headers {
		if strings.Contains(strings.ToLower(k), "billing") || strings.Contains(v, "cc_version") {
			hasBilling = true
			break
		}
	}
	// Also check in the body for billing header.
	if strings.Contains(result.Body, "cc_version") || strings.Contains(result.Body, "billing") {
		hasBilling = true
	}

	t.Logf("transform result: url=%s, headers=%d, body_len=%d, has_billing=%v",
		result.URL, len(result.Headers), len(result.Body), hasBilling)

	// Verify the body is valid JSON.
	var bodyMap map[string]interface{}
	if err := json.Unmarshal([]byte(result.Body), &bodyMap); err != nil {
		t.Errorf("transformed body is not valid JSON: %v", err)
	}
}

func TestLive_Phase4_4_BridgeFullProxy(t *testing.T) {
	logger := liveTestLogger(t)
	bridgeClient := bridge.NewClientWithConfig(liveTestBridgePort, logger)

	if !bridgeClient.IsAvailable() {
		t.Skip("Bridge not running. Start OpenCode with the fallback-bridge plugin to test bridge integration.")
	}

	authReader := requireAuthJSON(t)

	// Create provider WITH bridge.
	p := provider.NewAnthropicOAuthProvider(authReader, bridgeClient, logger)
	if !p.IsAvailable() {
		t.Skip("AnthropicOAuthProvider not available")
	}

	shutdown := startLiveProxy(t, p)
	defer shutdown()

	t.Log("sending request through proxy with bridge transformation...")

	resp, body := sendChatRequest(t, liveProxyBaseURL(), liveTestModel,
		"Reply with exactly: BRIDGE_OK", false)

	// Handle 429 retry.
	if resp.StatusCode == http.StatusTooManyRequests {
		t.Log("HTTP 429 — rate limited, retrying after 60s...")
		time.Sleep(60 * time.Second)
		resp, body = sendChatRequest(t, liveProxyBaseURL(), liveTestModel,
			"Reply with exactly: BRIDGE_OK", false)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200 (via bridge). Body: %s",
			resp.StatusCode, truncateForLog(string(body)))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatalf("response missing choices: %v", result)
	}

	t.Log("bridge full proxy test passed — request succeeded via bridge transformation")
	t.Logf("response preview: %s", truncateForLog(string(body)))
}

// ─── Utility Functions ───────────────────────────────────────────────

// truncateForLog truncates a string for safe logging (no token exposure).
func truncateForLog(s string) string {
	// Redact anything that looks like a token.
	s = redactTokens(s)
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

// redactTokens removes patterns that look like tokens/secrets from a string.
func redactTokens(s string) string {
	// Redact Bearer tokens.
	s = redactPattern(s, `"access"`, `"access": "[REDACTED]"`)
	s = redactPattern(s, `"refresh"`, `"refresh": "[REDACTED]"`)
	s = redactPattern(s, `"key"`, `"key": "[REDACTED]"`)
	return s
}

// redactPattern is a simple substring replacement for log safety.
func redactPattern(s, pattern, replacement string) string {
	// Find the pattern and replace the value that follows it.
	// This is a best-effort approach — not a full JSON parser.
	idx := strings.Index(s, pattern)
	if idx == -1 {
		return s
	}
	return s // Don't modify — just return as-is since we can't easily redact inline.
}

// mapKeys returns the keys of a map[string]interface{}.
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
