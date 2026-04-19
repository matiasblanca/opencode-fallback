package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	// Write a test config.
	original := `{
  "provider": {
    "anthropic": {
      "apiKey": "env:ANTHROPIC_API_KEY"
    },
    "openai": {
      "apiKey": "env:OPENAI_API_KEY"
    }
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Load.
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Save to a new path.
	newPath := filepath.Join(dir, "opencode2.json")
	if err := cfg.Save(newPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify saved file exists.
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("saved file does not exist")
	}
}

func TestLoadNonExistent(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Load() should return error for non-existent file")
	}
}

func TestSetProviderBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	original := `{
  "provider": {
    "anthropic": {
      "apiKey": "env:ANTHROPIC_API_KEY"
    }
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Set baseURL.
	if err := cfg.SetProviderBaseURL("anthropic", "http://localhost:8787/v1"); err != nil {
		t.Fatalf("SetProviderBaseURL() error = %v", err)
	}

	got := cfg.GetProviderBaseURL("anthropic")
	if got != "http://localhost:8787/v1" {
		t.Errorf("GetProviderBaseURL() = %q, want %q", got, "http://localhost:8787/v1")
	}

	// Save and reload.
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cfg2, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after save error = %v", err)
	}
	got2 := cfg2.GetProviderBaseURL("anthropic")
	if got2 != "http://localhost:8787/v1" {
		t.Errorf("GetProviderBaseURL() after reload = %q, want %q", got2, "http://localhost:8787/v1")
	}
}

func TestSetProviderBaseURLNewProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	// Minimal config without provider section.
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.SetProviderBaseURL("openai", "http://localhost:8787/v1"); err != nil {
		t.Fatalf("SetProviderBaseURL() error = %v", err)
	}

	got := cfg.GetProviderBaseURL("openai")
	if got != "http://localhost:8787/v1" {
		t.Errorf("GetProviderBaseURL() = %q, want %q", got, "http://localhost:8787/v1")
	}
}

func TestGetProviderBaseURLEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.GetProviderBaseURL("nonexistent")
	if got != "" {
		t.Errorf("GetProviderBaseURL() = %q, want empty", got)
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"test":true}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	backupPath, err := Backup(path)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("backup file does not exist")
	}

	// Verify backup content matches original.
	original, _ := os.ReadFile(path)
	backup, _ := os.ReadFile(backupPath)
	if string(original) != string(backup) {
		t.Errorf("backup content = %q, want %q", string(backup), string(original))
	}
}

func TestFindConfigPath(t *testing.T) {
	// This test just verifies it returns a non-empty string.
	path := FindConfigPath()
	if path == "" {
		t.Error("FindConfigPath() returned empty string")
	}
}
