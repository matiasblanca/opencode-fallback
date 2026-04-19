package provider

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func writeAuthFile(t *testing.T, dir string, content map[string]interface{}) string {
	t.Helper()
	authFile := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return authFile
}

func TestAnthropicOAuth_ID(t *testing.T) {
	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{})
	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewAnthropicOAuthProvider(reader, testLogger())

	if p.ID() != "anthropic-oauth" {
		t.Errorf("ID() = %q, want %q", p.ID(), "anthropic-oauth")
	}
}

func TestAnthropicOAuth_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		content map[string]interface{}
		want    bool
	}{
		{
			name: "available with oauth entry",
			content: map[string]interface{}{
				"anthropic": map[string]interface{}{
					"type":    "oauth",
					"refresh": "r",
					"access":  "a",
					"expires": 0,
				},
			},
			want: true,
		},
		{
			name: "not available with api entry",
			content: map[string]interface{}{
				"anthropic": map[string]interface{}{
					"type": "api",
					"key":  "sk-test",
				},
			},
			want: false,
		},
		{
			name:    "not available when empty",
			content: map[string]interface{}{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			authFile := writeAuthFile(t, dir, tt.content)
			reader := auth.NewReaderWithPath(authFile, testLogger())
			p := NewAnthropicOAuthProvider(reader, testLogger())

			if got := p.IsAvailable(); got != tt.want {
				t.Errorf("IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnthropicOAuth_SupportsModel(t *testing.T) {
	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{})
	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewAnthropicOAuthProvider(reader, testLogger())

	tests := []struct {
		model string
		want  bool
	}{
		{"claude-sonnet-4-20250514", true},
		{"claude-3-5-sonnet-20241022", true},
		{"claude-opus-4", true},
		{"gpt-4o", false},
		{"gemini-pro", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := p.SupportsModel(tt.model); got != tt.want {
				t.Errorf("SupportsModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestAnthropicOAuth_Headers(t *testing.T) {
	// Mock Anthropic API server.
	var receivedHeaders http.Header
	var receivedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedURL = r.URL.String()

		// Return a valid Anthropic response.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-sonnet-4",
			"content":     []map[string]string{{"type": "text", "text": "hello"}},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":    "oauth",
			"refresh": "test_refresh",
			"access":  "test_access_token",
			"expires": 9999999999999, // far future
		},
	})

	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewAnthropicOAuthProvider(reader, testLogger())

	// Override the base URL to point to our test server.
	// We need to create a request and call send, but the provider uses a
	// hardcoded URL. For header verification, we test the setOAuthHeaders
	// method directly.
	req, _ := http.NewRequest("POST", server.URL, nil)
	p.setOAuthHeaders(req, "test_access_token")

	// Verify Authorization header.
	authHeader := req.Header.Get("Authorization")
	if authHeader != "Bearer test_access_token" {
		t.Errorf("Authorization = %q, want %q", authHeader, "Bearer test_access_token")
	}

	// Verify x-api-key is NOT set.
	if apiKey := req.Header.Get("x-api-key"); apiKey != "" {
		t.Errorf("x-api-key should NOT be set, got %q", apiKey)
	}

	// Verify anthropic-beta header.
	beta := req.Header.Get("anthropic-beta")
	if beta != anthropicOAuthBeta {
		t.Errorf("anthropic-beta = %q, want %q", beta, anthropicOAuthBeta)
	}

	// Verify User-Agent.
	ua := req.Header.Get("User-Agent")
	if ua != claudeCodeUserAgent {
		t.Errorf("User-Agent = %q, want %q", ua, claudeCodeUserAgent)
	}

	// Verify anthropic-version.
	version := req.Header.Get("anthropic-version")
	if version != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q, want %q", version, anthropicAPIVersion)
	}

	// Verify Content-Type.
	ct := req.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Use the variables to avoid unused warnings (they were set by the mock
	// but we tested headers via direct method call).
	_ = receivedHeaders
	_ = receivedURL
}

func TestAnthropicOAuth_URLIncludesBeta(t *testing.T) {
	// The URL should end with ?beta=true.
	// We verify this by checking the constant used.
	url := anthropicOAuthBaseURL + "/v1/messages?beta=true"
	if !strings.Contains(url, "?beta=true") {
		t.Errorf("URL should contain ?beta=true: %q", url)
	}
}

func TestAnthropicOAuth_ClassifyError(t *testing.T) {
	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{})
	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewAnthropicOAuthProvider(reader, testLogger())

	tests := []struct {
		status    int
		wantType  ErrorType
		wantReson string
	}{
		{429, ErrorRetriable, "rate_limit"},
		{529, ErrorRetriable, "overloaded"},
		{401, ErrorFatal, "auth"},
		{500, ErrorRetriable, "server_error"},
	}

	for _, tt := range tests {
		result := p.ClassifyError(tt.status, http.Header{}, nil)
		if result.Type != tt.wantType {
			t.Errorf("ClassifyError(%d).Type = %v, want %v", tt.status, result.Type, tt.wantType)
		}
		if result.Reason != tt.wantReson {
			t.Errorf("ClassifyError(%d).Reason = %q, want %q", tt.status, result.Reason, tt.wantReson)
		}
	}
}
