// Package bridge provides an HTTP client for the opencode-fallback-bridge
// plugin. The bridge is an OpenCode plugin that exposes a local HTTP
// endpoint for auth token retrieval and request transformation.
//
// The bridge is an OPTIMIZATION, not a requirement. When unavailable,
// the proxy falls back to its own Go-based implementation.
//
// Dependency rules: bridge/ imports auth/ (for types only).
// bridge/ MUST NOT import provider/, fallback/, proxy/, or transform/.
package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
)

const (
	// defaultBridgePort is the default port the bridge plugin listens on.
	defaultBridgePort = 18787

	// bridgeTimeout is the timeout for all bridge HTTP requests.
	bridgeTimeout = 5 * time.Second

	// healthCacheTTL is how long a health check result is cached.
	healthCacheTTL = 30 * time.Second

	// bridgeTokenFilename is the name of the token file written by the plugin.
	bridgeTokenFilename = "fallback-bridge-token"
)

// TransformResult is the response from the bridge's /transform/anthropic endpoint.
type TransformResult struct {
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
	URL     string            `json:"url"`
}

// Client communicates with the opencode-fallback-bridge plugin.
type Client struct {
	baseURL string
	token   string
	client  *http.Client
	logger  *slog.Logger

	// Health check cache.
	mu              sync.Mutex
	available       bool
	healthCheckedAt time.Time
}

// NewClient creates a new bridge client.
//
// It reads the bridge port from FALLBACK_BRIDGE_PORT env var (default: 18787)
// and the bearer token from <XDG_DATA_HOME>/opencode/fallback-bridge-token.
// If the token file doesn't exist, the bridge is considered unavailable.
func NewClient(logger *slog.Logger) *Client {
	port := defaultBridgePort
	if portStr := os.Getenv("FALLBACK_BRIDGE_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			port = p
		}
	}

	token := readTokenFile(logger)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	return &Client{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: bridgeTimeout},
		logger:  logger,
	}
}

// NewClientWithConfig creates a bridge client with explicit config values.
// Used when config specifies a custom port.
func NewClientWithConfig(port int, logger *slog.Logger) *Client {
	if port <= 0 {
		port = defaultBridgePort
	}

	token := readTokenFile(logger)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	return &Client{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: bridgeTimeout},
		logger:  logger,
	}
}

// IsAvailable checks if the bridge plugin is running.
// The result is cached for 30 seconds.
func (c *Client) IsAvailable() bool {
	// No token → bridge can't be used.
	if c.token == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached result if fresh enough.
	if !c.healthCheckedAt.IsZero() && time.Since(c.healthCheckedAt) < healthCacheTTL {
		return c.available
	}

	// Perform health check.
	c.available = c.doHealthCheck()
	c.healthCheckedAt = time.Now()

	return c.available
}

// InvalidateHealthCache forces the next IsAvailable() call to re-check.
func (c *Client) InvalidateHealthCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthCheckedAt = time.Time{}
}

// GetAuth retrieves fresh auth tokens from the bridge for the given provider.
func (c *Client) GetAuth(providerID string) (*auth.AuthEntry, error) {
	url := c.baseURL + "/auth/" + providerID

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build auth request: %w", err)
	}
	c.setBearerAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bridge auth response: %w", err)
	}

	if resp.StatusCode == 404 {
		return nil, nil // provider not found — not an error
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bridge auth returned HTTP %d", resp.StatusCode)
	}

	// Parse the response based on type.
	var raw struct {
		Type    string `json:"type"`
		Access  string `json:"access,omitempty"`
		Expires int64  `json:"expires,omitempty"`
		Key     string `json:"key,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse bridge auth response: %w", err)
	}

	switch raw.Type {
	case "oauth":
		return &auth.AuthEntry{
			Type: "oauth",
			OAuth: &auth.OAuthData{
				Access:  raw.Access,
				Expires: raw.Expires,
			},
		}, nil
	case "api":
		return &auth.AuthEntry{
			Type: "api",
			API:  &auth.APIData{Key: raw.Key},
		}, nil
	default:
		return nil, fmt.Errorf("unknown auth type from bridge: %s", raw.Type)
	}
}

// TransformAnthropic sends a request body to the bridge for full Claude Code
// transformation. Returns the transformed body, headers, and URL.
func (c *Client) TransformAnthropic(body string) (*TransformResult, error) {
	url := c.baseURL + "/transform/anthropic"

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build transform request: %w", err)
	}
	c.setBearerAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge transform request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bridge transform response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bridge transform returned HTTP %d: %s",
			resp.StatusCode, truncateBody(string(respBody)))
	}

	var result TransformResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse bridge transform response: %w", err)
	}

	return &result, nil
}

// ─── Internal Methods ─────────────────────────────────────────────────

// doHealthCheck performs a single health check request to the bridge.
func (c *Client) doHealthCheck() bool {
	url := c.baseURL + "/health"

	resp, err := c.client.Get(url)
	if err != nil {
		c.logger.Debug("bridge health check failed",
			"error", err,
		)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.logger.Debug("bridge health check returned non-200",
			"status", resp.StatusCode,
		)
		return false
	}

	var health struct {
		Status string `json:"status"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &health); err != nil || health.Status != "ok" {
		c.logger.Debug("bridge health check invalid response")
		return false
	}

	c.logger.Debug("bridge is available")
	return true
}

// setBearerAuth sets the Authorization header with the bridge token.
func (c *Client) setBearerAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
}

// ─── Token File ───────────────────────────────────────────────────────

// readTokenFile reads the bridge token from the well-known file path.
// Returns empty string if the file doesn't exist.
func readTokenFile(logger *slog.Logger) string {
	path := bridgeTokenPath()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		logger.Debug("bridge token file not found (bridge not running)",
			"path", path,
		)
		return ""
	}
	if err != nil {
		logger.Warn("failed to read bridge token file",
			"path", path,
			"error", err,
		)
		return ""
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		logger.Warn("bridge token file is empty",
			"path", path,
		)
		return ""
	}

	logger.Debug("bridge token loaded",
		"path", path,
	)
	return token
}

// bridgeTokenPath returns the platform-specific path for the bridge token file.
func bridgeTokenPath() string {
	if override := os.Getenv("OPENCODE_DATA_DIR"); override != "" {
		return filepath.Join(override, bridgeTokenFilename)
	}

	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			home, _ := os.UserHomeDir()
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "opencode", bridgeTokenFilename)

	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "opencode", bridgeTokenFilename)

	default: // linux and others
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "opencode", bridgeTokenFilename)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "opencode", bridgeTokenFilename)
	}
}

// ─── Utilities ────────────────────────────────────────────────────────

// truncateBody truncates a response body string for error messages.
func truncateBody(body string) string {
	if len(body) > 200 {
		return body[:200] + "..."
	}
	return body
}

// ReloadToken re-reads the bridge token file from disk.
// Called when the bridge becomes unavailable and we want to check for a new token.
func (c *Client) ReloadToken() {
	c.token = readTokenFile(c.logger)
	c.InvalidateHealthCache()
}

// SetBaseURL overrides the base URL (for testing).
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

// SetToken overrides the bearer token (for testing).
func (c *Client) SetToken(token string) {
	c.token = token
}

// NewTestClient creates a Client with custom base URL and token (for testing).
func NewTestClient(baseURL, token string, logger *slog.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: bridgeTimeout,
		},
		logger: logger,
	}
}


