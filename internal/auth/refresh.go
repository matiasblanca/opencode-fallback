// Package auth — refresh.go implements Anthropic OAuth token refresh.
//
// When the access token in auth.json is expired, this module contacts
// Anthropic's OAuth endpoint to obtain fresh tokens using the refresh
// token. Updated tokens are written back to auth.json atomically.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// anthropicTokenEndpoint is the OAuth token refresh endpoint.
	anthropicTokenEndpoint = "https://platform.claude.com/v1/oauth/token"

	// anthropicClientID is the OAuth client ID used by Claude Code.
	anthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

	// refreshMaxRetries is the maximum number of retries (3 attempts total).
	refreshMaxRetries = 2

	// refreshUserAgent matches the User-Agent used by the anthropic-auth plugin.
	refreshUserAgent = "axios/1.13.6"
)

// refreshBackoffs defines the backoff durations between retries.
var refreshBackoffs = []time.Duration{500 * time.Millisecond, 1000 * time.Millisecond}

// refreshRequest is the JSON body sent to the token endpoint.
type refreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
}

// refreshResponse is the JSON body received from the token endpoint.
type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

// Refresher handles token refresh for Anthropic OAuth tokens.
// It uses a sync.Mutex to deduplicate concurrent refresh attempts.
type Refresher struct {
	mu       sync.Mutex
	reader   *Reader
	client   *http.Client
	logger   *slog.Logger
	endpoint string // injectable for testing
}

// NewRefresher creates a Refresher for Anthropic OAuth tokens.
func NewRefresher(reader *Reader, logger *slog.Logger) *Refresher {
	return &Refresher{
		reader:   reader,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
		endpoint: anthropicTokenEndpoint,
	}
}

// NewRefresherWithEndpoint creates a Refresher with a custom endpoint (for testing).
func NewRefresherWithEndpoint(reader *Reader, endpoint string, logger *slog.Logger) *Refresher {
	return &Refresher{
		reader:   reader,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
		endpoint: endpoint,
	}
}

// IsExpired reports whether the given OAuth entry has an expired access token.
// Returns false if expires == 0 (no expiry, e.g. GitHub Copilot).
func IsExpired(oauth *OAuthData) bool {
	if oauth.Expires == 0 {
		return false
	}
	return oauth.Expires < time.Now().UnixMilli()
}

// EnsureFresh checks the Anthropic auth entry and refreshes if expired.
// Returns the current (possibly refreshed) AuthEntry.
// The mutex ensures concurrent callers don't all refresh simultaneously.
func (r *Refresher) EnsureFresh(providerID string) (*AuthEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, err := r.reader.Get(providerID)
	if err != nil {
		return nil, fmt.Errorf("read auth for refresh: %w", err)
	}
	if entry == nil {
		return nil, nil
	}
	if entry.Type != "oauth" || entry.OAuth == nil {
		return entry, nil
	}

	if !IsExpired(entry.OAuth) {
		return entry, nil
	}

	r.logger.Info("access token expired, refreshing",
		"provider", providerID,
		"expired_at", entry.OAuth.Expires,
	)

	// Re-read before each attempt to avoid using stale refresh tokens.
	for attempt := 0; attempt <= refreshMaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry.
			time.Sleep(refreshBackoffs[attempt-1])

			// Re-read fresh auth in case another process refreshed.
			r.reader.InvalidateCache()
			entry, err = r.reader.Get(providerID)
			if err != nil {
				return nil, fmt.Errorf("re-read auth before retry: %w", err)
			}
			if entry == nil || entry.Type != "oauth" || entry.OAuth == nil {
				return nil, fmt.Errorf("auth entry disappeared during refresh")
			}
			// If someone else refreshed successfully, use their token.
			if !IsExpired(entry.OAuth) {
				r.logger.Info("token refreshed by another process",
					"provider", providerID,
				)
				return entry, nil
			}
		}

		newTokens, err := r.doRefresh(entry.OAuth.Refresh)
		if err != nil {
			if isRetriableRefreshError(err) {
				r.logger.Warn("token refresh failed (retriable), retrying",
					"provider", providerID,
					"attempt", attempt+1,
					"error", err,
				)
				continue
			}
			// Non-retriable error (4xx) — fail immediately.
			return nil, fmt.Errorf("token refresh failed: %w", err)
		}

		// Calculate new expiry.
		expiresAt := time.Now().UnixMilli() + newTokens.ExpiresIn*1000

		// Update the entry.
		updated := &AuthEntry{
			Type: "oauth",
			OAuth: &OAuthData{
				Refresh:       newTokens.RefreshToken,
				Access:        newTokens.AccessToken,
				Expires:       expiresAt,
				AccountID:     entry.OAuth.AccountID,
				EnterpriseURL: entry.OAuth.EnterpriseURL,
			},
		}

		// Write back to auth.json atomically.
		if err := r.reader.UpdateEntry(providerID, updated); err != nil {
			r.logger.Error("failed to write refreshed tokens",
				"provider", providerID,
				"error", err,
			)
			// Return the new tokens anyway — they're valid even if we
			// couldn't persist them.
			return updated, nil
		}

		r.logger.Info("token refreshed successfully",
			"provider", providerID,
			"expires_at", expiresAt,
		)

		return updated, nil
	}

	return nil, fmt.Errorf("token refresh exhausted all retries for provider %s", providerID)
}

// doRefresh performs a single token refresh request.
func (r *Refresher) doRefresh(refreshToken string) (*refreshResponse, error) {
	reqBody := refreshRequest{
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
		ClientID:     anthropicClientID,
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, r.endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("build refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", refreshUserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, &refreshNetworkError{err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &refreshHTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var tokens refreshResponse
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	return &tokens, nil
}

// refreshHTTPError represents an HTTP error from the token endpoint.
type refreshHTTPError struct {
	StatusCode int
	Body       string
}

func (e *refreshHTTPError) Error() string {
	return fmt.Sprintf("refresh endpoint returned HTTP %d: %s", e.StatusCode, e.Body)
}

// refreshNetworkError represents a network-level error during refresh.
type refreshNetworkError struct {
	err error
}

func (e *refreshNetworkError) Error() string {
	return fmt.Sprintf("refresh network error: %v", e.err)
}

func (e *refreshNetworkError) Unwrap() error {
	return e.err
}

// isRetriableRefreshError checks if a refresh error is retriable.
// Only HTTP 5xx and network errors are retriable; 4xx errors are not.
func isRetriableRefreshError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors are always retriable.
	if _, ok := err.(*refreshNetworkError); ok {
		return true
	}

	// HTTP errors: only 5xx are retriable.
	if httpErr, ok := err.(*refreshHTTPError); ok {
		return httpErr.StatusCode >= 500
	}

	// Check wrapped errors for network-related strings.
	errStr := err.Error()
	networkPatterns := []string{
		"connection refused", "connection reset",
		"timeout", "ECONNREFUSED", "ECONNRESET", "ETIMEDOUT",
	}
	for _, pattern := range networkPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}
