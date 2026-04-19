package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefresher_SuccessfulRefresh(t *testing.T) {
	// Mock server returns new tokens.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(400)
			return
		}

		if req.GrantType != "refresh_token" {
			t.Errorf("grant_type = %q, want %q", req.GrantType, "refresh_token")
		}
		if req.ClientID != anthropicClientID {
			t.Errorf("client_id = %q, want %q", req.ClientID, anthropicClientID)
		}

		// Verify headers.
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}
		if ua := r.Header.Get("User-Agent"); ua != refreshUserAgent {
			t.Errorf("User-Agent = %q, want %q", ua, refreshUserAgent)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new_access_token",
			"refresh_token": "new_refresh_token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	// Write expired tokens.
	content := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":    "oauth",
			"refresh": "old_refresh",
			"access":  "old_access",
			"expires": 100, // long expired
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reader := NewReaderWithPath(authFile, logger)
	refresher := NewRefresherWithEndpoint(reader, server.URL, logger)

	entry, err := refresher.EnsureFresh("anthropic")
	if err != nil {
		t.Fatalf("EnsureFresh failed: %v", err)
	}
	if entry == nil {
		t.Fatal("entry is nil")
	}
	if entry.OAuth.Access != "new_access_token" {
		t.Errorf("access = %q, want %q", entry.OAuth.Access, "new_access_token")
	}
	if entry.OAuth.Refresh != "new_refresh_token" {
		t.Errorf("refresh = %q, want %q", entry.OAuth.Refresh, "new_refresh_token")
	}

	// Verify the file was updated.
	reader.InvalidateCache()
	persisted, _ := reader.Get("anthropic")
	if persisted.OAuth.Access != "new_access_token" {
		t.Errorf("persisted access = %q, want %q", persisted.OAuth.Access, "new_access_token")
	}
}

func TestRefresher_SkipWhenNotExpired(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	// Write tokens with future expiry.
	futureExpiry := time.Now().Add(1 * time.Hour).UnixMilli()
	content := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":    "oauth",
			"refresh": "my_refresh",
			"access":  "my_access",
			"expires": futureExpiry,
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	logger := testLogger()
	reader := NewReaderWithPath(authFile, logger)
	refresher := NewRefresherWithEndpoint(reader, "http://should-not-be-called", logger)

	entry, err := refresher.EnsureFresh("anthropic")
	if err != nil {
		t.Fatalf("EnsureFresh failed: %v", err)
	}
	if entry.OAuth.Access != "my_access" {
		t.Errorf("access = %q, want %q (should not refresh)", entry.OAuth.Access, "my_access")
	}
}

func TestRefresher_SkipWhenExpiresZero(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	// expires=0 means "no expiry" (GitHub Copilot pattern).
	content := map[string]interface{}{
		"github-copilot": map[string]interface{}{
			"type":    "oauth",
			"refresh": "gho_token",
			"access":  "gho_token",
			"expires": 0,
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	logger := testLogger()
	reader := NewReaderWithPath(authFile, logger)
	refresher := NewRefresherWithEndpoint(reader, "http://should-not-be-called", logger)

	entry, err := refresher.EnsureFresh("github-copilot")
	if err != nil {
		t.Fatalf("EnsureFresh failed: %v", err)
	}
	if entry.OAuth.Access != "gho_token" {
		t.Errorf("access = %q, want %q", entry.OAuth.Access, "gho_token")
	}
}

func TestRefresher_RetryOn5xx(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"internal"}`))
			return
		}
		// Third attempt succeeds.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "retried_access",
			"refresh_token": "retried_refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")
	content := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":    "oauth",
			"refresh": "old_refresh",
			"access":  "old_access",
			"expires": 100,
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	logger := testLogger()
	reader := NewReaderWithPath(authFile, logger)
	refresher := NewRefresherWithEndpoint(reader, server.URL, logger)

	entry, err := refresher.EnsureFresh("anthropic")
	if err != nil {
		t.Fatalf("EnsureFresh failed: %v", err)
	}
	if entry.OAuth.Access != "retried_access" {
		t.Errorf("access = %q, want %q", entry.OAuth.Access, "retried_access")
	}
	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
}

func TestRefresher_NoRetryOn401(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")
	content := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":    "oauth",
			"refresh": "bad_refresh",
			"access":  "bad_access",
			"expires": 100,
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	logger := testLogger()
	reader := NewReaderWithPath(authFile, logger)
	refresher := NewRefresherWithEndpoint(reader, server.URL, logger)

	_, err := refresher.EnsureFresh("anthropic")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("attempts = %d, want 1 (should not retry 401)", n)
	}
}

func TestRefresher_ConcurrentDeduplication(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Small delay to simulate real request.
		time.Sleep(50 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "concurrent_access",
			"refresh_token": "concurrent_refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")
	content := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":    "oauth",
			"refresh": "old_refresh",
			"access":  "old_access",
			"expires": 100,
		},
	}
	data, _ := json.Marshal(content)
	os.WriteFile(authFile, data, 0o600)

	logger := testLogger()
	reader := NewReaderWithPath(authFile, logger)
	refresher := NewRefresherWithEndpoint(reader, server.URL, logger)

	// Launch 5 concurrent refresh attempts.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := refresher.EnsureFresh("anthropic")
			if err != nil {
				t.Errorf("EnsureFresh failed: %v", err)
			}
		}()
	}
	wg.Wait()

	// The mutex ensures serialization. The first call refreshes, subsequent
	// calls should see the fresh token and skip refresh.
	n := atomic.LoadInt32(&callCount)
	if n > 2 {
		t.Errorf("refresh called %d times, expected at most 2 (deduplication via mutex)", n)
	}
}

func TestIsExpired(t *testing.T) {
	tests := []struct {
		name    string
		expires int64
		want    bool
	}{
		{"zero means no expiry", 0, false},
		{"past time is expired", 100, true},
		{"future time is not expired", time.Now().Add(1 * time.Hour).UnixMilli(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oauth := &OAuthData{Expires: tt.expires}
			if got := IsExpired(oauth); got != tt.want {
				t.Errorf("IsExpired(%d) = %v, want %v", tt.expires, got, tt.want)
			}
		})
	}
}
