package provider

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
)

func TestCopilot_ID(t *testing.T) {
	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{})
	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewCopilotProvider(reader, testLogger())

	if p.ID() != "github-copilot" {
		t.Errorf("ID() = %q, want %q", p.ID(), "github-copilot")
	}
}

func TestCopilot_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		content map[string]interface{}
		want    bool
	}{
		{
			name: "available with oauth entry",
			content: map[string]interface{}{
				"github-copilot": map[string]interface{}{
					"type":    "oauth",
					"refresh": "gho_token",
					"access":  "gho_token",
					"expires": 0,
				},
			},
			want: true,
		},
		{
			name:    "not available when empty",
			content: map[string]interface{}{},
			want:    false,
		},
		{
			name: "not available with api entry",
			content: map[string]interface{}{
				"github-copilot": map[string]interface{}{
					"type": "api",
					"key":  "some-key",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			authFile := writeAuthFile(t, dir, tt.content)
			reader := auth.NewReaderWithPath(authFile, testLogger())
			p := NewCopilotProvider(reader, testLogger())

			if got := p.IsAvailable(); got != tt.want {
				t.Errorf("IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCopilot_SupportsModel(t *testing.T) {
	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{})
	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewCopilotProvider(reader, testLogger())

	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", true},
		{"gpt-4", true},
		{"claude-sonnet-4", true},
		{"claude-3-opus-20240229", true},
		{"gemini-pro", true},
		{"o1-preview", true},
		{"o3-mini", true},
		{"o4-mini", true},
		{"mistral-large", false},
		{"llama-3", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := p.SupportsModel(tt.model); got != tt.want {
				t.Errorf("SupportsModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestCopilot_Headers(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")
	content := map[string]interface{}{
		"github-copilot": map[string]interface{}{
			"type":    "oauth",
			"refresh": "gho_test_token",
			"access":  "gho_test_token",
			"expires": 0,
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewCopilotProvider(reader, testLogger())

	// Build a request to verify headers.
	req := &ProxyRequest{
		RawBody: json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
	}

	httpReq, err := p.buildRequest(t.Context(), req)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	// Verify Authorization.
	authHeader := httpReq.Header.Get("Authorization")
	if authHeader != "Bearer gho_test_token" {
		t.Errorf("Authorization = %q, want %q", authHeader, "Bearer gho_test_token")
	}

	// Verify User-Agent.
	ua := httpReq.Header.Get("User-Agent")
	if ua != copilotUserAgent {
		t.Errorf("User-Agent = %q, want %q", ua, copilotUserAgent)
	}

	// Verify Openai-Intent.
	intent := httpReq.Header.Get("Openai-Intent")
	if intent != "conversation-edits" {
		t.Errorf("Openai-Intent = %q, want %q", intent, "conversation-edits")
	}

	// Verify x-initiator.
	initiator := httpReq.Header.Get("x-initiator")
	if initiator != "user" {
		t.Errorf("x-initiator = %q, want %q", initiator, "user")
	}

	// Verify x-api-key is NOT set.
	if apiKey := httpReq.Header.Get("x-api-key"); apiKey != "" {
		t.Errorf("x-api-key should NOT be set, got %q", apiKey)
	}
}

func TestCopilot_EnterpriseURL(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")
	content := map[string]interface{}{
		"github-copilot": map[string]interface{}{
			"type":          "oauth",
			"refresh":       "gho_enterprise_token",
			"access":        "gho_enterprise_token",
			"expires":       0,
			"enterpriseUrl": "company.ghe.com",
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewCopilotProvider(reader, testLogger())

	// BaseURL should reflect enterprise.
	if got := p.BaseURL(); got != "https://copilot-api.company.ghe.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://copilot-api.company.ghe.com")
	}

	// Build request and verify URL.
	req := &ProxyRequest{
		RawBody: json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
	}

	httpReq, err := p.buildRequest(t.Context(), req)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	expectedURL := "https://copilot-api.company.ghe.com/chat/completions"
	if httpReq.URL.String() != expectedURL {
		t.Errorf("URL = %q, want %q", httpReq.URL.String(), expectedURL)
	}
}

func TestCopilot_ClassifyError(t *testing.T) {
	dir := t.TempDir()
	authFile := writeAuthFile(t, dir, map[string]interface{}{})
	reader := auth.NewReaderWithPath(authFile, testLogger())
	p := NewCopilotProvider(reader, testLogger())

	tests := []struct {
		status   int
		wantType ErrorType
	}{
		{429, ErrorRetriable},
		{401, ErrorFatal},
		{500, ErrorRetriable},
		{404, ErrorFatal},
	}

	for _, tt := range tests {
		result := p.ClassifyError(tt.status, http.Header{}, nil)
		if result.Type != tt.wantType {
			t.Errorf("ClassifyError(%d).Type = %v, want %v", tt.status, result.Type, tt.wantType)
		}
	}
}
