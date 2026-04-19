package bridge

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ─── Health Check Tests ───────────────────────────────────────────────

func TestHealthCheck_Available(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	if !client.IsAvailable() {
		t.Error("expected bridge to be available")
	}
}

func TestHealthCheck_Unavailable(t *testing.T) {
	// Use a server that immediately closes.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	if client.IsAvailable() {
		t.Error("expected bridge to be unavailable when health returns 500")
	}
}

func TestHealthCheck_NoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "", testLogger())

	if client.IsAvailable() {
		t.Error("expected bridge to be unavailable when no token")
	}
}

func TestHealthCheck_Cached(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			callCount++
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	// First call: health check.
	client.IsAvailable()
	// Second call: should use cache.
	client.IsAvailable()

	if callCount != 1 {
		t.Errorf("expected 1 health check call, got %d", callCount)
	}
}

func TestHealthCheck_InvalidateCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			callCount++
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	client.IsAvailable()
	client.InvalidateHealthCache()
	client.IsAvailable()

	if callCount != 2 {
		t.Errorf("expected 2 health check calls after invalidation, got %d", callCount)
	}
}

func TestHealthCheck_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	if client.IsAvailable() {
		t.Error("expected bridge to be unavailable when health returns bad JSON")
	}
}

func TestHealthCheck_WrongStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "degraded"})
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	if client.IsAvailable() {
		t.Error("expected bridge to be unavailable when health status is not 'ok'")
	}
}

// ─── GetAuth Tests ────────────────────────────────────────────────────

func TestGetAuth_OAuthEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/anthropic" {
			// Verify bearer token.
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-token" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"type":    "oauth",
				"access":  "fresh-access-token",
				"expires": 9999999999999,
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	entry, err := client.GetAuth("anthropic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Type != "oauth" {
		t.Errorf("type = %q, want %q", entry.Type, "oauth")
	}
	if entry.OAuth == nil {
		t.Fatal("expected OAuth data, got nil")
	}
	if entry.OAuth.Access != "fresh-access-token" {
		t.Errorf("access = %q, want %q", entry.OAuth.Access, "fresh-access-token")
	}
}

func TestGetAuth_APIEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/openai" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"type": "api",
				"key":  "sk-test-key",
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	entry, err := client.GetAuth("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Type != "api" {
		t.Errorf("type = %q, want %q", entry.Type, "api")
	}
	if entry.API == nil {
		t.Fatal("expected API data, got nil")
	}
	if entry.API.Key != "sk-test-key" {
		t.Errorf("key = %q, want %q", entry.API.Key, "sk-test-key")
	}
}

func TestGetAuth_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	entry, err := client.GetAuth("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error for missing provider: %v", err)
	}
	if entry != nil {
		t.Error("expected nil entry for missing provider")
	}
}

func TestGetAuth_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	_, err := client.GetAuth("anthropic")
	if err == nil {
		t.Error("expected error for server error response")
	}
}

// ─── TransformAnthropic Tests ─────────────────────────────────────────

func TestTransformAnthropic_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/transform/anthropic" && r.Method == "POST" {
			// Verify bearer token.
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-token" {
				w.WriteHeader(401)
				return
			}

			json.NewEncoder(w).Encode(TransformResult{
				Body: `{"system":[{"type":"text","text":"transformed"}],"messages":[]}`,
				Headers: map[string]string{
					"authorization":    "Bearer fresh-token",
					"anthropic-beta":   "oauth-2025-04-20",
					"user-agent":       "claude-cli/2.1.87 (external, cli)",
					"anthropic-version": "2023-06-01",
				},
				URL: "https://api.anthropic.com/v1/messages?beta=true",
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	result, err := client.TransformAnthropic(`{"messages":[{"role":"user","content":"hello"}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.URL != "https://api.anthropic.com/v1/messages?beta=true" {
		t.Errorf("url = %q, want Anthropic URL", result.URL)
	}
	if result.Headers["authorization"] != "Bearer fresh-token" {
		t.Errorf("authorization header = %q, want %q", result.Headers["authorization"], "Bearer fresh-token")
	}
	if result.Body == "" {
		t.Error("expected non-empty body")
	}
}

func TestTransformAnthropic_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "no oauth auth"})
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())

	_, err := client.TransformAnthropic(`{"messages":[]}`)
	if err == nil {
		t.Error("expected error for 400 response")
	}
}

// ─── Timeout Tests ────────────────────────────────────────────────────

func TestTransformAnthropic_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow response.
		time.Sleep(10 * time.Second)
		json.NewEncoder(w).Encode(TransformResult{Body: "{}", URL: "http://example.com"})
	}))
	defer server.Close()

	client := NewTestClient(server.URL, "test-token", testLogger())
	// Override timeout for testing.
	client.client.Timeout = 100 * time.Millisecond

	_, err := client.TransformAnthropic(`{"messages":[]}`)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// ─── Token File Tests ─────────────────────────────────────────────────

func TestReadTokenFile_NotExist(t *testing.T) {
	// Set OPENCODE_DATA_DIR to a temp dir with no token file.
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)

	token := readTokenFile(testLogger())
	if token != "" {
		t.Errorf("expected empty token for missing file, got %q", token)
	}
}

func TestReadTokenFile_Exists(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, bridgeTokenFilename)
	if err := os.WriteFile(tokenPath, []byte("my-secret-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OPENCODE_DATA_DIR", dir)

	token := readTokenFile(testLogger())
	if token != "my-secret-token" {
		t.Errorf("token = %q, want %q", token, "my-secret-token")
	}
}

func TestReadTokenFile_Empty(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, bridgeTokenFilename)
	if err := os.WriteFile(tokenPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OPENCODE_DATA_DIR", dir)

	token := readTokenFile(testLogger())
	if token != "" {
		t.Errorf("expected empty token for empty file, got %q", token)
	}
}

func TestReadTokenFile_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, bridgeTokenFilename)
	if err := os.WriteFile(tokenPath, []byte("  my-token  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OPENCODE_DATA_DIR", dir)

	token := readTokenFile(testLogger())
	if token != "my-token" {
		t.Errorf("token = %q, want %q", token, "my-token")
	}
}

// ─── ReloadToken Tests ────────────────────────────────────────────────

func TestReloadToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)

	client := NewClient(testLogger())

	// Initially no token.
	if client.token != "" {
		t.Fatalf("expected empty token initially, got %q", client.token)
	}

	// Write token file.
	tokenPath := filepath.Join(dir, bridgeTokenFilename)
	if err := os.WriteFile(tokenPath, []byte("new-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	client.ReloadToken()

	if client.token != "new-token" {
		t.Errorf("token after reload = %q, want %q", client.token, "new-token")
	}
}

// ─── NewClient Tests ──────────────────────────────────────────────────

func TestNewClient_DefaultPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)
	t.Setenv("FALLBACK_BRIDGE_PORT", "")

	client := NewClient(testLogger())
	expected := "http://127.0.0.1:18787"
	if client.baseURL != expected {
		t.Errorf("baseURL = %q, want %q", client.baseURL, expected)
	}
}

func TestNewClient_CustomPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)
	t.Setenv("FALLBACK_BRIDGE_PORT", "9999")

	client := NewClient(testLogger())
	expected := "http://127.0.0.1:9999"
	if client.baseURL != expected {
		t.Errorf("baseURL = %q, want %q", client.baseURL, expected)
	}
}

func TestNewClientWithConfig_CustomPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)

	client := NewClientWithConfig(12345, testLogger())
	expected := "http://127.0.0.1:12345"
	if client.baseURL != expected {
		t.Errorf("baseURL = %q, want %q", client.baseURL, expected)
	}
}

// ─── Bearer Auth Verification ─────────────────────────────────────────

func TestGetAuth_BearerRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer correct-token" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type":   "oauth",
			"access": "token",
		})
	}))
	defer server.Close()

	// With correct token.
	client := NewTestClient(server.URL, "correct-token", testLogger())
	entry, err := client.GetAuth("test")
	if err != nil {
		t.Fatalf("unexpected error with correct token: %v", err)
	}
	if entry == nil {
		t.Error("expected entry with correct token")
	}

	// With wrong token.
	wrongClient := NewTestClient(server.URL, "wrong-token", testLogger())
	_, err = wrongClient.GetAuth("test")
	if err == nil {
		t.Error("expected error with wrong token")
	}
}

// ─── truncateBody Tests ───────────────────────────────────────────────

func TestTruncateBody(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int // expected max length
	}{
		{"short", "hello", 5},
		{"exact 200", string(make([]byte, 200)), 200},
		{"long", string(make([]byte, 300)), 203}, // 200 + "..."
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateBody(tt.input)
			if len(result) > tt.want {
				t.Errorf("len(truncateBody) = %d, want <= %d", len(result), tt.want)
			}
		})
	}
}
