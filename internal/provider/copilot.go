// Package provider — copilot.go implements the CopilotProvider which uses
// GitHub OAuth tokens from OpenCode's auth.json to authenticate with the
// GitHub Copilot API. The Copilot API uses OpenAI-compatible format, so
// no request translation is needed.
package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

const (
	// copilotDefaultBaseURL is the default GitHub Copilot API endpoint.
	copilotDefaultBaseURL = "https://api.githubcopilot.com"

	// copilotProviderID is the unique provider identifier.
	copilotProviderID = "github-copilot"

	// copilotUserAgent is the User-Agent sent to GitHub Copilot.
	copilotUserAgent = "opencode/1.0.0"
)

// CopilotProvider implements the Provider interface for GitHub Copilot.
// It reads OAuth tokens from OpenCode's auth.json.
type CopilotProvider struct {
	authReader *auth.Reader
	client     *http.Client
	logger     *slog.Logger
}

// NewCopilotProvider creates a new GitHub Copilot provider.
func NewCopilotProvider(authReader *auth.Reader, logger *slog.Logger) *CopilotProvider {
	return &CopilotProvider{
		authReader: authReader,
		client:     &http.Client{},
		logger:     logger,
	}
}

// ID returns "github-copilot".
func (p *CopilotProvider) ID() string { return copilotProviderID }

// Name returns "GitHub Copilot".
func (p *CopilotProvider) Name() string { return "GitHub Copilot" }

// BaseURL returns the resolved Copilot API base URL.
// If the auth entry has an EnterpriseURL, it uses that instead.
func (p *CopilotProvider) BaseURL() string {
	entry, err := p.authReader.Get("github-copilot")
	if err != nil || entry == nil || entry.OAuth == nil {
		return copilotDefaultBaseURL
	}
	if entry.OAuth.EnterpriseURL != "" {
		return "https://copilot-api." + entry.OAuth.EnterpriseURL
	}
	return copilotDefaultBaseURL
}

// IsAvailable reports whether auth.json has an OAuth entry for "github-copilot".
func (p *CopilotProvider) IsAvailable() bool {
	entry, err := p.authReader.Get("github-copilot")
	if err != nil || entry == nil {
		return false
	}
	return entry.Type == "oauth" && entry.OAuth != nil
}

// SupportsModel reports whether Copilot supports the given model.
// Copilot supports all major model families.
func (p *CopilotProvider) SupportsModel(modelID string) bool {
	prefixes := []string{"gpt-", "claude-", "gemini-", "o1-", "o3-", "o4-"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(modelID, prefix) {
			return true
		}
	}
	return false
}

// ClassifyError classifies an HTTP error response using standard HTTP
// status code classification.
func (p *CopilotProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	return ClassifyGenericOpenAIError(statusCode, headers, body)
}

// Send sends a non-streaming request to GitHub Copilot.
func (p *CopilotProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
	httpReq, err := p.buildRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &ProviderError{
			ProviderID: copilotProviderID,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return &ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
	}, nil
}

// SendStream sends a streaming request to GitHub Copilot.
func (p *CopilotProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
	httpReq, err := p.buildRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &ProviderError{
			ProviderID: copilotProviderID,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return stream.NewSSEParser(resp.Body), nil
}

// buildRequest creates the HTTP request for GitHub Copilot.
func (p *CopilotProvider) buildRequest(ctx context.Context, req *ProxyRequest) (*http.Request, error) {
	// Get GitHub token — no refresh needed (tokens don't expire).
	entry, err := p.authReader.Get("github-copilot")
	if err != nil {
		return nil, fmt.Errorf("read copilot auth: %w", err)
	}
	if entry == nil || entry.OAuth == nil {
		return nil, fmt.Errorf("no copilot auth entry found")
	}

	// Use the Refresh field which contains the GitHub access token.
	token := entry.OAuth.Refresh

	// Determine base URL (handles Enterprise).
	baseURL := copilotDefaultBaseURL
	if entry.OAuth.EnterpriseURL != "" {
		baseURL = "https://copilot-api." + entry.OAuth.EnterpriseURL
	}

	url := baseURL + "/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(req.RawBody))
	if err != nil {
		return nil, err
	}

	// Set Copilot-specific headers.
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", copilotUserAgent)
	httpReq.Header.Set("Openai-Intent", "conversation-edits")
	httpReq.Header.Set("x-initiator", "user")
	// Note: x-api-key is NOT set.

	return httpReq, nil
}
