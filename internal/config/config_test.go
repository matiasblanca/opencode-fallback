package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/config"
)

// ─────────────────────────────────────────────────────────
// TestDefaultConfig — verify sensible defaults match the spec
// ─────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()

	// Proxy defaults
	if cfg.Proxy.Port != 8787 {
		t.Errorf("Proxy.Port = %d, want 8787", cfg.Proxy.Port)
	}
	if cfg.Proxy.Host != "127.0.0.1" {
		t.Errorf("Proxy.Host = %q, want \"127.0.0.1\"", cfg.Proxy.Host)
	}
	if cfg.Proxy.LogLevel != "info" {
		t.Errorf("Proxy.LogLevel = %q, want \"info\"", cfg.Proxy.LogLevel)
	}

	// Circuit breaker defaults
	if cfg.CircuitBreaker.FailureThreshold != 3 {
		t.Errorf("CircuitBreaker.FailureThreshold = %d, want 3", cfg.CircuitBreaker.FailureThreshold)
	}
	if cfg.CircuitBreaker.FailureWindowSeconds != 60 {
		t.Errorf("CircuitBreaker.FailureWindowSeconds = %d, want 60", cfg.CircuitBreaker.FailureWindowSeconds)
	}
	if cfg.CircuitBreaker.OpenDurationSeconds != 30 {
		t.Errorf("CircuitBreaker.OpenDurationSeconds = %d, want 30", cfg.CircuitBreaker.OpenDurationSeconds)
	}

	// Timeout defaults
	if cfg.Timeouts.ConnectSeconds != 5 {
		t.Errorf("Timeouts.ConnectSeconds = %d, want 5", cfg.Timeouts.ConnectSeconds)
	}
	if cfg.Timeouts.FirstTokenSeconds != 60 {
		t.Errorf("Timeouts.FirstTokenSeconds = %d, want 60", cfg.Timeouts.FirstTokenSeconds)
	}
	if cfg.Timeouts.HeartbeatSeconds != 30 {
		t.Errorf("Timeouts.HeartbeatSeconds = %d, want 30", cfg.Timeouts.HeartbeatSeconds)
	}
	if cfg.Timeouts.ReasoningModelMultiplier != 2.0 {
		t.Errorf("Timeouts.ReasoningModelMultiplier = %f, want 2.0", cfg.Timeouts.ReasoningModelMultiplier)
	}

	// Stream recovery defaults
	if !cfg.StreamRecovery.Enabled {
		t.Error("StreamRecovery.Enabled = false, want true")
	}
	want := "Continue exactly from where you left off. Do not repeat any content."
	if cfg.StreamRecovery.ContinuationPrompt != want {
		t.Errorf("StreamRecovery.ContinuationPrompt = %q, want %q", cfg.StreamRecovery.ContinuationPrompt, want)
	}

	// Providers must be an initialized (possibly empty) map
	if cfg.Providers == nil {
		t.Error("Providers map is nil, want empty map")
	}

	// Version must be "1"
	if cfg.Version != "1" {
		t.Errorf("Version = %q, want \"1\"", cfg.Version)
	}
}

// ─────────────────────────────────────────────────────────
// TestLoadNonExistent — missing file ⇒ zero-config returns defaults
// ─────────────────────────────────────────────────────────

func TestLoadNonExistent(t *testing.T) {
	// Override config path to a temp dir that has no config.json
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	defaults := config.DefaultConfig()
	if cfg.Proxy.Port != defaults.Proxy.Port {
		t.Errorf("Proxy.Port = %d, want %d", cfg.Proxy.Port, defaults.Proxy.Port)
	}
	if cfg.CircuitBreaker.FailureThreshold != defaults.CircuitBreaker.FailureThreshold {
		t.Errorf("CircuitBreaker.FailureThreshold = %d, want %d",
			cfg.CircuitBreaker.FailureThreshold, defaults.CircuitBreaker.FailureThreshold)
	}
}

// ─────────────────────────────────────────────────────────
// TestLoadFromFile — write a temp JSON, verify it loads correctly
// ─────────────────────────────────────────────────────────

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)

	raw := config.Config{
		Version: "1",
		Proxy:   config.ProxyConfig{Port: 9000, Host: "0.0.0.0", LogLevel: "debug"},
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL: "https://api.anthropic.com",
				APIKey:  "sk-test",
				Models:  []string{"claude-opus-4-5"},
			},
		},
		FallbackChains: config.FallbackChainsConfig{
			Global: []config.ChainEntry{
				{Provider: "anthropic", Model: "claude-opus-4-5"},
			},
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:     5,
			FailureWindowSeconds: 120,
			OpenDurationSeconds:  60,
		},
		Timeouts: config.TimeoutConfig{
			ConnectSeconds:           10,
			FirstTokenSeconds:        90,
			HeartbeatSeconds:         45,
			ReasoningModelMultiplier: 3.0,
		},
		StreamRecovery: config.StreamRecoveryConfig{
			Enabled:            true,
			ContinuationPrompt: "Continue.",
		},
	}

	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Proxy.Port != 9000 {
		t.Errorf("Proxy.Port = %d, want 9000", cfg.Proxy.Port)
	}
	if cfg.Proxy.LogLevel != "debug" {
		t.Errorf("Proxy.LogLevel = %q, want \"debug\"", cfg.Proxy.LogLevel)
	}
	if cfg.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("CircuitBreaker.FailureThreshold = %d, want 5", cfg.CircuitBreaker.FailureThreshold)
	}
	if cfg.Timeouts.ReasoningModelMultiplier != 3.0 {
		t.Errorf("Timeouts.ReasoningModelMultiplier = %f, want 3.0", cfg.Timeouts.ReasoningModelMultiplier)
	}
	if p, ok := cfg.Providers["anthropic"]; !ok {
		t.Error("providers[anthropic] not found")
	} else if p.APIKey != "sk-test" {
		t.Errorf("providers[anthropic].APIKey = %q, want \"sk-test\"", p.APIKey)
	}
}

// ─────────────────────────────────────────────────────────
// TestSaveAndLoad — round-trip: Save then Load produces same data
// ─────────────────────────────────────────────────────────

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)

	original := config.DefaultConfig()
	original.Proxy.Port = 7777
	original.Providers = map[string]config.ProviderConfig{
		"openai": {BaseURL: "https://api.openai.com", APIKey: "sk-openai"},
	}
	original.FallbackChains.Global = []config.ChainEntry{
		{Provider: "openai", Model: "gpt-4o"},
	}

	if err := config.Save(original); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() after Save() error = %v, want nil", err)
	}

	if loaded.Proxy.Port != 7777 {
		t.Errorf("round-trip Proxy.Port = %d, want 7777", loaded.Proxy.Port)
	}
	if p, ok := loaded.Providers["openai"]; !ok {
		t.Error("round-trip: providers[openai] not found")
	} else if p.APIKey != "sk-openai" {
		t.Errorf("round-trip: providers[openai].APIKey = %q, want \"sk-openai\"", p.APIKey)
	}
	if len(loaded.FallbackChains.Global) != 1 {
		t.Errorf("round-trip: global chain len = %d, want 1", len(loaded.FallbackChains.Global))
	} else if loaded.FallbackChains.Global[0].Provider != "openai" {
		t.Errorf("round-trip: global[0].Provider = %q, want \"openai\"", loaded.FallbackChains.Global[0].Provider)
	}
}

// ─────────────────────────────────────────────────────────
// TestResolveEnvVars — "$ENV_VAR" references are resolved at load time
// ─────────────────────────────────────────────────────────

func TestResolveEnvVars(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)
	t.Setenv("MY_TEST_KEY", "resolved-secret")

	raw := config.Config{
		Version: "1",
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL: "https://api.anthropic.com",
				APIKey:  "$MY_TEST_KEY",
				Models:  []string{"claude-opus-4-5"},
			},
		},
		FallbackChains: config.FallbackChainsConfig{
			Global: []config.ChainEntry{
				{Provider: "anthropic", Model: "claude-opus-4-5"},
			},
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:     3,
			FailureWindowSeconds: 60,
			OpenDurationSeconds:  30,
		},
	}

	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Providers["anthropic"].APIKey != "resolved-secret" {
		t.Errorf("APIKey = %q, want \"resolved-secret\"", cfg.Providers["anthropic"].APIKey)
	}
}

// ─────────────────────────────────────────────────────────
// TestResolveEnvVarsLiteral — literal keys (no "$") stay as-is
// ─────────────────────────────────────────────────────────

func TestResolveEnvVarsLiteral(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)

	raw := config.Config{
		Version: "1",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				BaseURL: "https://api.openai.com",
				APIKey:  "sk-literal-key",
				Models:  []string{"gpt-4o"},
			},
		},
		FallbackChains: config.FallbackChainsConfig{
			Global: []config.ChainEntry{
				{Provider: "openai", Model: "gpt-4o"},
			},
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:     3,
			FailureWindowSeconds: 60,
			OpenDurationSeconds:  30,
		},
	}

	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Providers["openai"].APIKey != "sk-literal-key" {
		t.Errorf("APIKey = %q, want \"sk-literal-key\"", cfg.Providers["openai"].APIKey)
	}
}

// ─────────────────────────────────────────────────────────
// TestResolveOAuthTokenEnvVar — "$TOKEN" in OAuthToken is resolved
// ─────────────────────────────────────────────────────────

func TestResolveOAuthTokenEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)
	t.Setenv("MY_OAUTH_TOKEN", "oauth-resolved")

	raw := config.Config{
		Version: "1",
		Providers: map[string]config.ProviderConfig{
			"mycloud": {
				BaseURL:    "https://cloud.example.com",
				OAuthToken: "$MY_OAUTH_TOKEN",
				Models:     []string{"model-x"},
			},
		},
		FallbackChains: config.FallbackChainsConfig{
			Global: []config.ChainEntry{
				{Provider: "mycloud", Model: "model-x"},
			},
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:     3,
			FailureWindowSeconds: 60,
			OpenDurationSeconds:  30,
		},
	}

	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Providers["mycloud"].OAuthToken != "oauth-resolved" {
		t.Errorf("OAuthToken = %q, want \"oauth-resolved\"", cfg.Providers["mycloud"].OAuthToken)
	}
}

// ─────────────────────────────────────────────────────────
// TestValidate — a valid config passes with no errors
// ─────────────────────────────────────────────────────────

func TestValidate(t *testing.T) {
	cfg := config.Config{
		Version: "1",
		Proxy:   config.ProxyConfig{Port: 8787, Host: "127.0.0.1", LogLevel: "info"},
		Providers: map[string]config.ProviderConfig{
			"anthropic": {BaseURL: "https://api.anthropic.com", APIKey: "sk-test"},
		},
		FallbackChains: config.FallbackChainsConfig{
			Global: []config.ChainEntry{
				{Provider: "anthropic", Model: "claude-opus-4-5"},
			},
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:     3,
			FailureWindowSeconds: 60,
			OpenDurationSeconds:  30,
		},
	}

	errs := config.Validate(cfg)
	if len(errs) != 0 {
		t.Errorf("Validate() errors = %v, want none", errs)
	}
}

// ─────────────────────────────────────────────────────────
// TestValidateErrors — invalid configs return the right errors
// ─────────────────────────────────────────────────────────

func TestValidateErrors(t *testing.T) {
	base := func() config.Config {
		return config.Config{
			Version: "1",
			Proxy:   config.ProxyConfig{Port: 8787, Host: "127.0.0.1", LogLevel: "info"},
			Providers: map[string]config.ProviderConfig{
				"anthropic": {BaseURL: "https://api.anthropic.com", APIKey: "sk-test"},
			},
			FallbackChains: config.FallbackChainsConfig{
				Global: []config.ChainEntry{
					{Provider: "anthropic", Model: "claude-opus-4-5"},
				},
			},
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold:     3,
				FailureWindowSeconds: 60,
				OpenDurationSeconds:  30,
			},
		}
	}

	cases := []struct {
		name       string
		mutate     func(*config.Config)
		wantSubstr string
	}{
		{
			name:       "wrong version",
			mutate:     func(c *config.Config) { c.Version = "2" },
			wantSubstr: "version",
		},
		{
			name:       "port zero",
			mutate:     func(c *config.Config) { c.Proxy.Port = 0 },
			wantSubstr: "port",
		},
		{
			name:       "port too high",
			mutate:     func(c *config.Config) { c.Proxy.Port = 70000 },
			wantSubstr: "port",
		},
		{
			name:       "empty host",
			mutate:     func(c *config.Config) { c.Proxy.Host = "" },
			wantSubstr: "host",
		},
		{
			name:       "no providers",
			mutate:     func(c *config.Config) { c.Providers = map[string]config.ProviderConfig{} },
			wantSubstr: "provider",
		},
		{
			name:       "empty global chain",
			mutate:     func(c *config.Config) { c.FallbackChains.Global = nil },
			wantSubstr: "global",
		},
		{
			name: "chain references missing provider",
			mutate: func(c *config.Config) {
				c.FallbackChains.Global = []config.ChainEntry{
					{Provider: "ghost", Model: "model-x"},
				}
			},
			wantSubstr: "ghost",
		},
		{
			name: "chain entry with empty model",
			mutate: func(c *config.Config) {
				c.FallbackChains.Global = []config.ChainEntry{
					{Provider: "anthropic", Model: ""},
				}
			},
			wantSubstr: "model",
		},
		{
			name: "circuit breaker threshold zero",
			mutate: func(c *config.Config) {
				c.CircuitBreaker.FailureThreshold = 0
			},
			wantSubstr: "failure_threshold",
		},
		{
			name: "circuit breaker window zero",
			mutate: func(c *config.Config) {
				c.CircuitBreaker.FailureWindowSeconds = 0
			},
			wantSubstr: "failure_window",
		},
		{
			name: "circuit breaker open duration zero",
			mutate: func(c *config.Config) {
				c.CircuitBreaker.OpenDurationSeconds = 0
			},
			wantSubstr: "open_duration",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg)

			errs := config.Validate(cfg)
			if len(errs) == 0 {
				t.Fatalf("Validate() returned no errors, want at least one containing %q", tc.wantSubstr)
			}

			found := false
			for _, e := range errs {
				if strings.Contains(strings.ToLower(e.Error()), tc.wantSubstr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Validate() errors %v do not contain %q", errs, tc.wantSubstr)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────
// TestConfigFile — config file path is within UserConfigDir
// ─────────────────────────────────────────────────────────

func TestConfigFile(t *testing.T) {
	path := config.ConfigFile()

	if path == "" {
		t.Fatal("ConfigFile() returned empty string")
	}

	// Must end with config.json
	if filepath.Base(path) != "config.json" {
		t.Errorf("ConfigFile() base = %q, want \"config.json\"", filepath.Base(path))
	}

	// Parent dir must be named "opencode-fallback"
	dir := filepath.Dir(path)
	if filepath.Base(dir) != "opencode-fallback" {
		t.Errorf("ConfigFile() parent dir = %q, want \"opencode-fallback\"", filepath.Base(dir))
	}
}

// ─────────────────────────────────────────────────────────
// TestConfigDir — config dir ends with "opencode-fallback"
// ─────────────────────────────────────────────────────────

func TestConfigDir(t *testing.T) {
	dir := config.ConfigDir()

	if dir == "" {
		t.Fatal("ConfigDir() returned empty string")
	}

	if filepath.Base(dir) != "opencode-fallback" {
		t.Errorf("ConfigDir() = %q, want base \"opencode-fallback\"", dir)
	}
}

// ─────────────────────────────────────────────────────────
// TestProviderConfigTypeField — backward compat and new type field
// ─────────────────────────────────────────────────────────

func TestProviderConfigTypeField_Omitted(t *testing.T) {
	// Old config without "type" field must deserialize without error.
	raw := `{"base_url":"https://api.openai.com","api_key":"sk-test","models":["gpt-4o"]}`
	var pc config.ProviderConfig
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if pc.Type != "" {
		t.Errorf("Type = %q, want empty (backward compat)", pc.Type)
	}
	if pc.BaseURL != "https://api.openai.com" {
		t.Errorf("BaseURL = %q", pc.BaseURL)
	}
}

func TestProviderConfigTypeField_OpenAICompatible(t *testing.T) {
	raw := `{"type":"openai-compatible","base_url":"https://api.mistral.ai","api_key":"key","models":["mistral-large-latest"]}`
	var pc config.ProviderConfig
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if pc.Type != "openai-compatible" {
		t.Errorf("Type = %q, want %q", pc.Type, "openai-compatible")
	}
}

func TestProviderConfigTypeField_Anthropic(t *testing.T) {
	raw := `{"type":"anthropic","base_url":"https://api.anthropic.com","api_key":"key"}`
	var pc config.ProviderConfig
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if pc.Type != "anthropic" {
		t.Errorf("Type = %q, want %q", pc.Type, "anthropic")
	}
}

func TestProviderConfigDisplayName(t *testing.T) {
	raw := `{"display_name":"My Custom Provider","base_url":"https://x.com","api_key":"key"}`
	var pc config.ProviderConfig
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if pc.DisplayName != "My Custom Provider" {
		t.Errorf("DisplayName = %q, want %q", pc.DisplayName, "My Custom Provider")
	}
}

func TestProviderConfigRoundTrip_WithType(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_FALLBACK_CONFIG_DIR", dir)

	original := config.DefaultConfig()
	original.Providers = map[string]config.ProviderConfig{
		"mistral": {
			Type:        "openai-compatible",
			DisplayName: "Mistral AI",
			BaseURL:     "https://api.mistral.ai",
			APIKey:      "sk-mistral",
			Models:      []string{"mistral-large-latest"},
		},
	}

	if err := config.Save(original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	p, ok := loaded.Providers["mistral"]
	if !ok {
		t.Fatal("providers[mistral] not found after round-trip")
	}
	if p.Type != "openai-compatible" {
		t.Errorf("Type = %q, want %q", p.Type, "openai-compatible")
	}
	if p.DisplayName != "Mistral AI" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "Mistral AI")
	}
}

// ─────────────────────────────────────────────────────────
// TestDetectAvailableProviders — env-based auto-detection
// ─────────────────────────────────────────────────────────

func TestDetectAvailableProviders(t *testing.T) {
	// Clear all provider env vars first, then set specific ones for the test
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	providers, chain := config.DetectAvailableProviders()

	// anthropic must be present
	ant, ok := providers["anthropic"]
	if !ok {
		t.Fatal("DetectAvailableProviders() missing anthropic provider")
	}
	if ant.BaseURL != "https://api.anthropic.com" {
		t.Errorf("anthropic.BaseURL = %q, want \"https://api.anthropic.com\"", ant.BaseURL)
	}
	if len(ant.Models) == 0 {
		t.Error("anthropic.Models is empty, want at least one model")
	}

	// openai must NOT be present (key empty)
	if _, ok := providers["openai"]; ok {
		t.Error("openai should not be detected when OPENAI_API_KEY is empty")
	}

	// chain must include anthropic
	if len(chain) == 0 {
		t.Fatal("chain is empty, want at least anthropic")
	}
	if chain[0].Provider != "anthropic" {
		t.Errorf("chain[0].Provider = %q, want \"anthropic\"", chain[0].Provider)
	}
}

func TestDetectAvailableProviders_Mistral(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MISTRAL_API_KEY", "sk-mistral-test")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	providers, _ := config.DetectAvailableProviders()

	m, ok := providers["mistral"]
	if !ok {
		t.Fatal("mistral not detected when MISTRAL_API_KEY is set")
	}
	if m.BaseURL != "https://api.mistral.ai" {
		t.Errorf("mistral.BaseURL = %q, want %q", m.BaseURL, "https://api.mistral.ai")
	}
	if len(m.Models) == 0 {
		t.Error("mistral.Models is empty")
	}
}

func TestDetectAvailableProviders_Gemini(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MISTRAL_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "gemini-key-test")
	t.Setenv("OPENROUTER_API_KEY", "")

	providers, _ := config.DetectAvailableProviders()

	g, ok := providers["gemini"]
	if !ok {
		t.Fatal("gemini not detected when GEMINI_API_KEY is set")
	}
	wantURL := "https://generativelanguage.googleapis.com/v1beta/openai"
	if g.BaseURL != wantURL {
		t.Errorf("gemini.BaseURL = %q, want %q", g.BaseURL, wantURL)
	}
	if len(g.Models) == 0 {
		t.Error("gemini.Models is empty")
	}
}

func TestDetectAvailableProviders_OpenRouter(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MISTRAL_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	providers, _ := config.DetectAvailableProviders()

	or, ok := providers["openrouter"]
	if !ok {
		t.Fatal("openrouter not detected when OPENROUTER_API_KEY is set")
	}
	if or.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("openrouter.BaseURL = %q, want %q", or.BaseURL, "https://openrouter.ai/api/v1")
	}
}

func TestDetectAvailableProviders_NotDetectedWhenEmpty(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	providers, _ := config.DetectAvailableProviders()

	if _, ok := providers["mistral"]; ok {
		t.Error("mistral should not be detected when key is empty")
	}
	if _, ok := providers["gemini"]; ok {
		t.Error("gemini should not be detected when key is empty")
	}
	if _, ok := providers["openrouter"]; ok {
		t.Error("openrouter should not be detected when key is empty")
	}
}

func TestDetectAvailableProvidersMultiple(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")
	t.Setenv("DEEPSEEK_API_KEY", "")

	providers, chain := config.DetectAvailableProviders()

	if _, ok := providers["anthropic"]; !ok {
		t.Error("anthropic not detected")
	}
	if _, ok := providers["openai"]; !ok {
		t.Error("openai not detected")
	}

	// Chain ordering: anthropic before openai
	providerOrder := make([]string, 0, len(chain))
	for _, e := range chain {
		// Collect unique providers from chain
		found := false
		for _, p := range providerOrder {
			if p == e.Provider {
				found = true
				break
			}
		}
		if !found {
			providerOrder = append(providerOrder, e.Provider)
		}
	}

	// anthropic must appear before openai in chain
	antIdx, openaiIdx := -1, -1
	for i, p := range providerOrder {
		if p == "anthropic" {
			antIdx = i
		}
		if p == "openai" {
			openaiIdx = i
		}
	}
	if antIdx == -1 || openaiIdx == -1 {
		t.Fatalf("chain providers = %v, want both anthropic and openai", providerOrder)
	}
	if antIdx > openaiIdx {
		t.Errorf("anthropic (idx=%d) should appear before openai (idx=%d) in chain", antIdx, openaiIdx)
	}
}
