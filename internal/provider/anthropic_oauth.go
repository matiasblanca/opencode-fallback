// Package provider — anthropic_oauth.go implements the AnthropicOAuthProvider
// which uses OAuth tokens from OpenCode's auth.json to authenticate with
// Anthropic's API. It applies Claude Code impersonation transformations
// to make the request pass as a Claude Code client.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/matiasblanca/opencode-fallback/internal/adapter"
	"github.com/matiasblanca/opencode-fallback/internal/auth"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
	"github.com/matiasblanca/opencode-fallback/internal/transform"
)

const (
	// anthropicOAuthBaseURL is the default Anthropic API base URL.
	anthropicOAuthBaseURL = "https://api.anthropic.com"

	// anthropicOAuthBeta is the beta header value for OAuth requests.
	anthropicOAuthBeta = "oauth-2025-04-20,interleaved-thinking-2025-05-14"

	// claudeCodeUserAgent is the User-Agent for Claude Code impersonation.
	claudeCodeUserAgent = "claude-cli/2.1.87 (external, cli)"

	// anthropicOAuthProviderID is the unique provider identifier.
	anthropicOAuthProviderID = "anthropic-oauth"
)

// AnthropicOAuthProvider implements the Provider interface for Anthropic
// using OAuth tokens read from OpenCode's auth.json.
type AnthropicOAuthProvider struct {
	authReader  *auth.Reader
	refresher   *auth.Refresher
	client      *http.Client
	logger      *slog.Logger
}

// NewAnthropicOAuthProvider creates a new Anthropic OAuth provider.
// It does NOT take an API key — credentials come from auth.json via the reader.
func NewAnthropicOAuthProvider(authReader *auth.Reader, logger *slog.Logger) *AnthropicOAuthProvider {
	return &AnthropicOAuthProvider{
		authReader: authReader,
		refresher:  auth.NewRefresher(authReader, logger),
		client:     &http.Client{},
		logger:     logger,
	}
}

// ID returns "anthropic-oauth" — distinct from "anthropic" (API key).
func (p *AnthropicOAuthProvider) ID() string { return anthropicOAuthProviderID }

// Name returns "Anthropic (OAuth)".
func (p *AnthropicOAuthProvider) Name() string { return "Anthropic (OAuth)" }

// BaseURL returns the Anthropic API base URL.
func (p *AnthropicOAuthProvider) BaseURL() string { return anthropicOAuthBaseURL }

// IsAvailable reports whether auth.json has an OAuth entry for "anthropic".
func (p *AnthropicOAuthProvider) IsAvailable() bool {
	entry, err := p.authReader.Get("anthropic")
	if err != nil || entry == nil {
		return false
	}
	return entry.Type == "oauth" && entry.OAuth != nil
}

// SupportsModel reports whether this provider supports the given model.
// Accepts any model starting with "claude-".
func (p *AnthropicOAuthProvider) SupportsModel(modelID string) bool {
	return strings.HasPrefix(modelID, "claude-")
}

// ClassifyError classifies an HTTP error response from Anthropic.
func (p *AnthropicOAuthProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	return ClassifyAnthropicError(statusCode, headers, body)
}

// Send sends a non-streaming request to Anthropic via OAuth.
func (p *AnthropicOAuthProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
	// 1. Get fresh auth.
	accessToken, err := p.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// 2. Translate OpenAI → Anthropic format.
	var openaiReq adapter.OpenAIRequest
	if err := json.Unmarshal(req.RawBody, &openaiReq); err != nil {
		return nil, fmt.Errorf("parse openai request: %w", err)
	}

	anthropicReq := adapter.ConvertOpenAIToAnthropic(openaiReq)
	anthropicReq.Stream = false

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	// 3. Apply Claude Code impersonation transformations.
	transformedBody, err := transform.RewriteRequestBody(string(body))
	if err != nil {
		return nil, fmt.Errorf("transform request body: %w", err)
	}

	// 4. Build HTTP request with ?beta=true.
	url := anthropicOAuthBaseURL + "/v1/messages?beta=true"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader(transformedBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	p.setOAuthHeaders(httpReq, accessToken)

	// 5. Send request.
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &ProviderError{
			ProviderID: anthropicOAuthProviderID,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       respBody,
		}
	}

	// 6. Translate Anthropic response → OpenAI format.
	var anthropicResp adapter.AnthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}

	openaiResp := adapter.ConvertAnthropicToOpenAI(anthropicResp)
	openaiBody, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, fmt.Errorf("marshal openai response: %w", err)
	}

	return &ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       openaiBody,
	}, nil
}

// SendStream sends a streaming request to Anthropic via OAuth.
// The response stream has tool prefixes stripped automatically.
func (p *AnthropicOAuthProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
	// 1. Get fresh auth.
	accessToken, err := p.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// 2. Translate OpenAI → Anthropic format.
	var openaiReq adapter.OpenAIRequest
	if err := json.Unmarshal(req.RawBody, &openaiReq); err != nil {
		return nil, fmt.Errorf("parse openai request: %w", err)
	}

	anthropicReq := adapter.ConvertOpenAIToAnthropic(openaiReq)
	anthropicReq.Stream = true

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	// 3. Apply Claude Code impersonation transformations.
	transformedBody, err := transform.RewriteRequestBody(string(body))
	if err != nil {
		return nil, fmt.Errorf("transform request body: %w", err)
	}

	// 4. Build HTTP request with ?beta=true.
	url := anthropicOAuthBaseURL + "/v1/messages?beta=true"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader(transformedBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	p.setOAuthHeaders(httpReq, accessToken)

	// 5. Send request.
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &ProviderError{
			ProviderID: anthropicOAuthProviderID,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       respBody,
		}
	}

	// 6. Wrap with tool prefix stripping.
	return stream.NewSSEParserWithTransform(resp.Body, transform.StripToolPrefix), nil
}

// setOAuthHeaders sets the Anthropic OAuth-specific request headers.
func (p *AnthropicOAuthProvider) setOAuthHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", anthropicOAuthBeta)
	req.Header.Set("User-Agent", claudeCodeUserAgent)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	// Note: x-api-key is NOT set — OAuth uses Authorization: Bearer.
}

// getAccessToken retrieves a fresh access token, refreshing if needed.
func (p *AnthropicOAuthProvider) getAccessToken() (string, error) {
	entry, err := p.refresher.EnsureFresh("anthropic")
	if err != nil {
		return "", err
	}
	if entry == nil || entry.OAuth == nil {
		return "", fmt.Errorf("no OAuth entry found for anthropic")
	}
	return entry.OAuth.Access, nil
}
