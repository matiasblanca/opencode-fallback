package auth

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestReader_GetOAuthEntry(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	content := `{
		"anthropic": {
			"type": "oauth",
			"refresh": "refresh_token_123",
			"access": "access_token_456",
			"expires": 1721234567890
		},
		"github-copilot": {
			"type": "oauth",
			"refresh": "gho_xxxxxxxxxxxx",
			"access": "gho_xxxxxxxxxxxx",
			"expires": 0,
			"enterpriseUrl": "company.ghe.com"
		},
		"openai": {
			"type": "api",
			"key": "sk-proj-test"
		}
	}`

	if err := os.WriteFile(authFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReaderWithPath(authFile, testLogger())

	tests := []struct {
		name       string
		providerID string
		wantType   string
		wantNil    bool
	}{
		{
			name:       "anthropic oauth entry",
			providerID: "anthropic",
			wantType:   "oauth",
		},
		{
			name:       "github-copilot oauth entry",
			providerID: "github-copilot",
			wantType:   "oauth",
		},
		{
			name:       "openai api entry",
			providerID: "openai",
			wantType:   "api",
		},
		{
			name:       "nonexistent provider returns nil",
			providerID: "nonexistent",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := reader.Get(tt.providerID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil {
				if entry != nil {
					t.Fatalf("expected nil entry, got %+v", entry)
				}
				return
			}

			if entry == nil {
				t.Fatal("expected non-nil entry")
			}
			if entry.Type != tt.wantType {
				t.Errorf("type = %q, want %q", entry.Type, tt.wantType)
			}
		})
	}

	// Verify specific fields.
	t.Run("anthropic oauth fields", func(t *testing.T) {
		entry, _ := reader.Get("anthropic")
		if entry.OAuth == nil {
			t.Fatal("OAuth data is nil")
		}
		if entry.OAuth.Refresh != "refresh_token_123" {
			t.Errorf("refresh = %q, want %q", entry.OAuth.Refresh, "refresh_token_123")
		}
		if entry.OAuth.Access != "access_token_456" {
			t.Errorf("access = %q, want %q", entry.OAuth.Access, "access_token_456")
		}
		if entry.OAuth.Expires != 1721234567890 {
			t.Errorf("expires = %d, want %d", entry.OAuth.Expires, 1721234567890)
		}
	})

	t.Run("copilot enterprise url", func(t *testing.T) {
		entry, _ := reader.Get("github-copilot")
		if entry.OAuth.EnterpriseURL != "company.ghe.com" {
			t.Errorf("enterpriseUrl = %q, want %q", entry.OAuth.EnterpriseURL, "company.ghe.com")
		}
		if entry.OAuth.Expires != 0 {
			t.Errorf("expires = %d, want 0", entry.OAuth.Expires)
		}
	})

	t.Run("openai api key", func(t *testing.T) {
		entry, _ := reader.Get("openai")
		if entry.API == nil {
			t.Fatal("API data is nil")
		}
		if entry.API.Key != "sk-proj-test" {
			t.Errorf("key = %q, want %q", entry.API.Key, "sk-proj-test")
		}
	})
}

func TestReader_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "auth.json")

	reader := NewReaderWithPath(path, testLogger())
	entry, err := reader.Get("anthropic")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if entry != nil {
		t.Fatalf("missing file should return nil entry, got %+v", entry)
	}
}

func TestReader_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	if err := os.WriteFile(authFile, []byte(`{invalid json`), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReaderWithPath(authFile, testLogger())
	_, err := reader.Get("anthropic")
	if err == nil {
		t.Fatal("malformed JSON should return error")
	}
}

func TestReader_XDGPathOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)

	authFile := filepath.Join(dir, "auth.json")
	content := `{"test": {"type": "api", "key": "test-key"}}`
	if err := os.WriteFile(authFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(testLogger())
	entry, err := reader.Get("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry from overridden path")
	}
	if entry.API.Key != "test-key" {
		t.Errorf("key = %q, want %q", entry.API.Key, "test-key")
	}
}

func TestReader_CacheTTL(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	content1 := `{"test": {"type": "api", "key": "key1"}}`
	if err := os.WriteFile(authFile, []byte(content1), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReaderWithPath(authFile, testLogger())

	// First read.
	entry, _ := reader.Get("test")
	if entry.API.Key != "key1" {
		t.Errorf("first read: key = %q, want %q", entry.API.Key, "key1")
	}

	// Update file — should be cached.
	content2 := `{"test": {"type": "api", "key": "key2"}}`
	if err := os.WriteFile(authFile, []byte(content2), 0o600); err != nil {
		t.Fatal(err)
	}

	// Within TTL — should still return old value.
	entry, _ = reader.Get("test")
	if entry.API.Key != "key1" {
		t.Errorf("cached read: key = %q, want %q (should be cached)", entry.API.Key, "key1")
	}

	// Invalidate cache.
	reader.InvalidateCache()
	entry, _ = reader.Get("test")
	if entry.API.Key != "key2" {
		t.Errorf("after invalidate: key = %q, want %q", entry.API.Key, "key2")
	}
}

func TestReader_UpdateEntry(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	content := `{"anthropic": {"type": "oauth", "refresh": "old_refresh", "access": "old_access", "expires": 100}}`
	if err := os.WriteFile(authFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReaderWithPath(authFile, testLogger())

	// Update the entry.
	newEntry := &AuthEntry{
		Type: "oauth",
		OAuth: &OAuthData{
			Refresh: "new_refresh",
			Access:  "new_access",
			Expires: 999,
		},
	}

	if err := reader.UpdateEntry("anthropic", newEntry); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Read back.
	entry, err := reader.Get("anthropic")
	if err != nil {
		t.Fatalf("read after update: %v", err)
	}
	if entry.OAuth.Refresh != "new_refresh" {
		t.Errorf("refresh = %q, want %q", entry.OAuth.Refresh, "new_refresh")
	}
	if entry.OAuth.Access != "new_access" {
		t.Errorf("access = %q, want %q", entry.OAuth.Access, "new_access")
	}
	if entry.OAuth.Expires != 999 {
		t.Errorf("expires = %d, want %d", entry.OAuth.Expires, 999)
	}
}

func TestReader_SkipsMalformedEntries(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	// Mix of valid and invalid entries.
	content := `{
		"valid": {"type": "api", "key": "good-key"},
		"bad-type": {"type": 123},
		"no-type": {"key": "orphan"}
	}`
	if err := os.WriteFile(authFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReaderWithPath(authFile, testLogger())

	// Valid entry should work.
	entry, err := reader.Get("valid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil || entry.API.Key != "good-key" {
		t.Error("valid entry should be readable")
	}

	// Bad entries should return nil (skipped).
	entry, err = reader.Get("bad-type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("bad-type should be skipped")
	}

	entry, err = reader.Get("no-type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("no-type should be skipped")
	}
}
