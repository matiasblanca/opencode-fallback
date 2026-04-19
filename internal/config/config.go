// Package config handles configuration loading, saving, and validation
// for opencode-fallback.
//
// This package is at the bottom of the dependency hierarchy — it MUST NOT
// import any other internal package.
//
// Config supports three modes:
//   - File-based: loads from ~/.config/opencode-fallback/config.json
//   - Zero-config: auto-detects providers from environment variables
//   - Defaults: sensible defaults for all settings
//
// API keys in config support env var references ("$ANTHROPIC_API_KEY")
// which are resolved at load time. Literal keys trigger a warning.
package config

import (
	"encoding/json"
	"os"
	"strings"
)

// Config is the main configuration struct for opencode-fallback.
type Config struct {
	Version        string                    `json:"version"`
	Proxy          ProxyConfig               `json:"proxy"`
	Providers      map[string]ProviderConfig `json:"providers"`
	FallbackChains FallbackChainsConfig      `json:"fallback_chains"`
	CircuitBreaker CircuitBreakerConfig      `json:"circuit_breaker"`
	Timeouts       TimeoutConfig             `json:"timeouts"`
	StreamRecovery StreamRecoveryConfig      `json:"stream_recovery"`
	Bridge         BridgeConfig              `json:"bridge"`
}

// BridgeConfig holds settings for the plugin bridge connection.
// The bridge is an OpenCode plugin that exposes a local HTTP endpoint for
// auth token retrieval and request transformation. When enabled, the proxy
// delegates Claude Code impersonation to the plugin instead of using its
// own Go-based implementation.
type BridgeConfig struct {
	// Enabled controls whether the proxy tries to use the bridge.
	// Default: true. When true, the proxy checks for the bridge on startup
	// but falls back to Go implementation if the bridge is not running.
	Enabled bool `json:"enabled"`
	// Port is the port the bridge plugin listens on. Default: 18787.
	Port int `json:"port"`
}

// ProxyConfig holds settings for the HTTP proxy server.
type ProxyConfig struct {
	Port     int    `json:"port"`
	Host     string `json:"host"`
	LogLevel string `json:"log_level"`
}

// ProviderConfig holds connection details for a single LLM provider.
type ProviderConfig struct {
	// Type identifies the provider protocol: "openai-compatible", "anthropic",
	// "anthropic-oauth", "github-copilot".
	// If empty, it is inferred from the provider name at buildRegistry time:
	//   - "anthropic" → "anthropic"
	//   - everything else → "openai-compatible"
	Type        string   `json:"type,omitempty"`
	// DisplayName is the human-readable name shown in logs.
	// Defaults to the provider map key if empty.
	DisplayName string   `json:"display_name,omitempty"`
	BaseURL     string   `json:"base_url,omitempty"`
	APIKey      string   `json:"api_key,omitempty"`
	AuthType    string   `json:"auth_type,omitempty"`
	OAuthToken  string   `json:"oauth_token,omitempty"`
	Models      []string `json:"models,omitempty"`
	// AuthSource specifies where to read credentials.
	// "env" (default) = env vars/config, "opencode" = read from OpenCode's auth.json
	AuthSource  string   `json:"auth_source,omitempty"`
}

// FallbackChainsConfig defines global and per-agent fallback chains.
type FallbackChainsConfig struct {
	Global []ChainEntry            `json:"_global"`
	Groups map[string][]ChainEntry `json:"_groups,omitempty"`
	Agents map[string][]ChainEntry `json:"agents,omitempty"`
}

// ChainEntry is a single step in a fallback chain.
type ChainEntry struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// CircuitBreakerConfig holds settings for the circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold     int `json:"failure_threshold"`
	FailureWindowSeconds int `json:"failure_window_seconds"`
	OpenDurationSeconds  int `json:"open_duration_seconds"`
}

// TimeoutConfig holds timeout settings for requests.
type TimeoutConfig struct {
	ConnectSeconds           int     `json:"connect_seconds"`
	FirstTokenSeconds        int     `json:"first_token_seconds"`
	HeartbeatSeconds         int     `json:"heartbeat_seconds"`
	ReasoningModelMultiplier float64 `json:"reasoning_model_multiplier"`
}

// StreamRecoveryConfig holds settings for stream recovery on interruptions.
type StreamRecoveryConfig struct {
	Enabled            bool   `json:"enabled"`
	ContinuationPrompt string `json:"continuation_prompt"`
}

// Load reads the configuration from disk.
//
// If the config file does not exist, Load returns DefaultConfig() (zero-config
// mode). If the file exists, it is parsed and env var references in API keys
// are resolved.
func Load() (Config, error) {
	path := ConfigFile()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	resolveEnvVars(&cfg)
	return cfg, nil
}

// Save writes the configuration to disk atomically.
//
// It creates the config directory if it does not exist, marshals the config
// to indented JSON, writes it to a temporary file, then atomically renames
// it to the final path.
func Save(cfg Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	path := ConfigFile()
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// resolveEnvVars expands env var references in provider credentials.
// A value starting with "$" is treated as an env var name and replaced
// with the result of os.Getenv.
func resolveEnvVars(cfg *Config) {
	resolved := make(map[string]ProviderConfig, len(cfg.Providers))
	for name, p := range cfg.Providers {
		if strings.HasPrefix(p.APIKey, "$") {
			p.APIKey = os.Getenv(p.APIKey[1:])
		}
		if strings.HasPrefix(p.OAuthToken, "$") {
			p.OAuthToken = os.Getenv(p.OAuthToken[1:])
		}
		resolved[name] = p
	}
	cfg.Providers = resolved
}


