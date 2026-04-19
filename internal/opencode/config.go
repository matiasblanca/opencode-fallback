package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// OpenCodeConfig represents the relevant parts of opencode.json.
// We use json.RawMessage to preserve unknown fields.
type OpenCodeConfig struct {
	raw map[string]json.RawMessage
}

// Load reads an opencode.json file from the given path.
func Load(path string) (*OpenCodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read opencode config: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}

	return &OpenCodeConfig{raw: raw}, nil
}

// Save writes the config to the given path atomically.
func (c *OpenCodeConfig) Save(path string) error {
	data, err := json.MarshalIndent(c.raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal opencode config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	return os.Rename(tmp, path)
}

// SetProviderBaseURL sets the baseURL for a given provider in the config.
// If the provider section doesn't exist, it creates it.
func (c *OpenCodeConfig) SetProviderBaseURL(providerName, baseURL string) error {
	// Get or create the "provider" section.
	providerSection := make(map[string]map[string]interface{})
	if raw, ok := c.raw["provider"]; ok {
		if err := json.Unmarshal(raw, &providerSection); err != nil {
			return fmt.Errorf("parse provider section: %w", err)
		}
	}

	// Get or create the specific provider entry.
	entry, ok := providerSection[providerName]
	if !ok {
		entry = make(map[string]interface{})
	}

	entry["baseURL"] = baseURL
	providerSection[providerName] = entry

	// Write back.
	data, err := json.Marshal(providerSection)
	if err != nil {
		return fmt.Errorf("marshal provider section: %w", err)
	}
	c.raw["provider"] = json.RawMessage(data)

	return nil
}

// GetProviderBaseURL returns the baseURL for a provider, or empty string if not set.
func (c *OpenCodeConfig) GetProviderBaseURL(providerName string) string {
	raw, ok := c.raw["provider"]
	if !ok {
		return ""
	}

	var providerSection map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &providerSection); err != nil {
		return ""
	}

	entry, ok := providerSection[providerName]
	if !ok {
		return ""
	}

	if url, ok := entry["baseURL"].(string); ok {
		return url
	}
	return ""
}

// Backup creates a timestamped backup of the given file.
// Returns the backup path.
func Backup(path string) (string, error) {
	backupPath := path + ".bak." + time.Now().UTC().Format("20060102T150405Z")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read for backup: %w", err)
	}
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return backupPath, nil
}

// FindConfigPath searches for opencode.json in standard locations.
// Returns the first existing path, or the default path if none exists.
func FindConfigPath() string {
	// Check local project config first.
	local := filepath.Join(".opencode", "config.json")
	if _, err := os.Stat(local); err == nil {
		return local
	}

	// Check global config.
	global := globalConfigPath()
	if _, err := os.Stat(global); err == nil {
		return global
	}

	// Default to global path.
	return global
}

// globalConfigPath returns the platform-specific global config path.
func globalConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "opencode", "config.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "opencode", "config.json")
	}
}
