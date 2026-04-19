package auth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// AuthEntry is the discriminated union for a provider's stored auth.
// Exactly one of OAuth or API is non-nil depending on the Type field.
type AuthEntry struct {
	Type  string     // "oauth" or "api"
	OAuth *OAuthData // non-nil when Type == "oauth"
	API   *APIData   // non-nil when Type == "api"
}

// OAuthData holds OAuth token data stored by OpenCode.
type OAuthData struct {
	Refresh       string `json:"refresh"`
	Access        string `json:"access"`
	Expires       int64  `json:"expires"` // milliseconds since epoch; 0 = no expiry
	AccountID     string `json:"accountId,omitempty"`
	EnterpriseURL string `json:"enterpriseUrl,omitempty"`
}

// APIData holds an API key stored by OpenCode.
type APIData struct {
	Key string `json:"key"`
}

// rawEntry is used to unmarshal a single entry and discriminate by type.
type rawEntry struct {
	Type string `json:"type"`
}

// Reader reads OpenCode's auth.json file with simple polling cache.
// It re-reads the file on each Get() call, caching for up to 5 seconds.
type Reader struct {
	mu        sync.Mutex
	logger    *slog.Logger
	cache     map[string]*AuthEntry
	cacheTime time.Time
	cacheTTL  time.Duration
	pathFunc  func() string // injectable for testing
}

// NewReader creates a Reader that reads auth.json from the default XDG path.
func NewReader(logger *slog.Logger) *Reader {
	return &Reader{
		logger:   logger,
		cacheTTL: 5 * time.Second,
		pathFunc: defaultAuthPath,
	}
}

// NewReaderWithPath creates a Reader that reads from a custom path (for testing).
func NewReaderWithPath(path string, logger *slog.Logger) *Reader {
	return &Reader{
		logger:   logger,
		cacheTTL: 5 * time.Second,
		pathFunc: func() string { return path },
	}
}

// Get returns the AuthEntry for the given provider ID.
// Returns (nil, nil) if the provider is not found — missing is not an error.
func (r *Reader) Get(providerID string) (*AuthEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensureLoaded(); err != nil {
		return nil, err
	}

	entry, ok := r.cache[providerID]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

// Path returns the resolved auth.json path.
func (r *Reader) Path() string {
	return r.pathFunc()
}

// ensureLoaded re-reads auth.json if the cache has expired.
// Must be called with r.mu held.
func (r *Reader) ensureLoaded() error {
	if r.cache != nil && time.Since(r.cacheTime) < r.cacheTTL {
		return nil
	}

	path := r.pathFunc()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		r.cache = make(map[string]*AuthEntry)
		r.cacheTime = time.Now()
		r.logger.Debug("auth.json not found, using empty auth", "path", path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read auth.json: %w", err)
	}

	// Check file permissions (warn but don't block).
	r.checkPermissions(path)

	entries, err := parseAuthFile(data)
	if err != nil {
		return fmt.Errorf("parse auth.json: %w", err)
	}

	r.cache = entries
	r.cacheTime = time.Now()

	r.logger.Debug("auth.json loaded",
		"path", path,
		"providers", len(entries),
	)

	return nil
}

// checkPermissions verifies the auth file permissions are 0o600.
// Logs a warning (does NOT block) if permissions are looser.
func (r *Reader) checkPermissions(path string) {
	if runtime.GOOS == "windows" {
		// Windows doesn't have Unix file permissions; skip check.
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	mode := info.Mode().Perm()
	if mode != 0o600 {
		r.logger.Warn("auth.json has loose permissions — should be 0600",
			"path", path,
			"permissions", fmt.Sprintf("%04o", mode),
		)
	}
}

// parseAuthFile parses the raw auth.json bytes into a map of provider
// entries. It uses a discriminated union pattern: it reads the "type"
// field first, then unmarshals into the correct struct.
func parseAuthFile(data []byte) (map[string]*AuthEntry, error) {
	// First pass: get raw JSON for each provider.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal auth.json: %w", err)
	}

	entries := make(map[string]*AuthEntry, len(raw))

	for providerID, rawJSON := range raw {
		// Discriminate by type field.
		var disc rawEntry
		if err := json.Unmarshal(rawJSON, &disc); err != nil {
			// Skip malformed entries.
			continue
		}

		switch disc.Type {
		case "oauth":
			var oauth OAuthData
			if err := json.Unmarshal(rawJSON, &oauth); err != nil {
				continue
			}
			entries[providerID] = &AuthEntry{
				Type:  "oauth",
				OAuth: &oauth,
			}

		case "api":
			var api struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(rawJSON, &api); err != nil {
				continue
			}
			entries[providerID] = &AuthEntry{
				Type: "api",
				API:  &APIData{Key: api.Key},
			}

		default:
			// Unknown type — skip silently.
		}
	}

	return entries, nil
}

// defaultAuthPath returns the platform-specific path for OpenCode's auth.json.
//
// Resolution order:
//  1. OPENCODE_DATA_DIR env var (if set)
//  2. Platform-specific XDG data dir:
//     - Linux:   ~/.local/share/opencode/auth.json
//     - macOS:   ~/Library/Application Support/opencode/auth.json
//     - Windows: %LOCALAPPDATA%/opencode/auth.json
func defaultAuthPath() string {
	if override := os.Getenv("OPENCODE_DATA_DIR"); override != "" {
		return filepath.Join(override, "auth.json")
	}

	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			home, _ := os.UserHomeDir()
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "opencode", "auth.json")

	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "opencode", "auth.json")

	default: // linux and others
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "opencode", "auth.json")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "opencode", "auth.json")
	}
}

// InvalidateCache forces the next Get() call to re-read from disk.
func (r *Reader) InvalidateCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheTime = time.Time{}
}

// UpdateEntry updates a single provider entry in auth.json atomically.
// It reads the current file, merges the change, and writes atomically.
func (r *Reader) UpdateEntry(providerID string, entry *AuthEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := r.pathFunc()

	// Read current file.
	var raw map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read auth.json for update: %w", err)
	}
	if data != nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse auth.json for update: %w", err)
		}
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}

	// Build the entry JSON.
	var entryJSON []byte
	switch entry.Type {
	case "oauth":
		entryJSON, err = json.Marshal(struct {
			Type          string `json:"type"`
			Refresh       string `json:"refresh"`
			Access        string `json:"access"`
			Expires       int64  `json:"expires"`
			AccountID     string `json:"accountId,omitempty"`
			EnterpriseURL string `json:"enterpriseUrl,omitempty"`
		}{
			Type:          "oauth",
			Refresh:       entry.OAuth.Refresh,
			Access:        entry.OAuth.Access,
			Expires:       entry.OAuth.Expires,
			AccountID:     entry.OAuth.AccountID,
			EnterpriseURL: entry.OAuth.EnterpriseURL,
		})
	case "api":
		entryJSON, err = json.Marshal(struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}{
			Type: "api",
			Key:  entry.API.Key,
		})
	default:
		return fmt.Errorf("unknown entry type: %s", entry.Type)
	}
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	raw[providerID] = entryJSON

	// Marshal complete file.
	output, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth.json: %w", err)
	}

	// Write atomically: write to .tmp, then rename.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, output, 0o600); err != nil {
		return fmt.Errorf("write tmp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("rename tmp to auth.json: %w", err)
	}

	// Invalidate cache so next Get() reads fresh data.
	r.cacheTime = time.Time{}

	return nil
}
