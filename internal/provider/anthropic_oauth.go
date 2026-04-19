// Package provider — anthropic_oauth.go implements the AnthropicOAuthProvider
// which uses OAuth tokens from OpenCode's auth.json to authenticate with
// Anthropic's API. It applies Claude Code impersonation transformations
// to make the request pass as a Claude Code client.
//
// When a bridge client is available, the provider delegates transformation
// to the plugin bridge (always up-to-date TypeScript). When the bridge is
// unavailable, it falls back to the Go-based transformation code.
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
	"github.com/matiasblanca/opencode-fallback/internal/bridge"
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
	authReader *auth.Reader
	refresher  *auth.Refresher
	bridge     *bridge.Client // optional — nil means bridge not configured
	client     *http.Client
	logger     *slog.Logger
}

// NewAnthropicOAuthProvider creates a new Anthropic OAuth provider.
// It does NOT take an API key — credentials come from auth.json via the reader.
// The bridge client is optional — pass nil to disable bridge support.
func NewAnthropicOAuthProvider(authReader *auth.Reader, bridgeClient *bridge.Client, logger *slog.Logger) *AnthropicOAuthProvider {
	return &AnthropicOAuthProvider{
		authReader: authReader,
		refresher:  auth.NewRefresher(authReader, logger),
		bridge:     bridgeClient,
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
// Tries the bridge first; falls back to Go implementation if unavailable.
func (p *AnthropicOAuthProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
	// Try bridge path first.
	if p.bridge != nil && p.bridge.IsAvailable() {
		resp, err := p.sendViaBridge(ctx, req, false)
		if err == nil {
			return resp, nil
		}
		p.logger.Warn("bridge transform failed, falling back to local",
			"error", err,
		)
	}

	// Fallback: Go implementation.
	return p.sendLocal(ctx, req)
}

// SendStream sends a streaming request to Anthropic via OAuth.
// Tries the bridge first; falls back to Go implementation if unavailable.
func (p *AnthropicOAuthProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
	// Try bridge path first.
	if p.bridge != nil && p.bridge.IsAvailable() {
		parser, err := p.sendStreamViaBridge(ctx, req)
		if err == nil {
			return parser, nil
		}
		p.logger.Warn("bridge transform failed for stream, falling back to local",
			"error", err,
		)
	}

	// Fallback: Go implementation.
	return p.sendStreamLocal(ctx, req)
}

// ─── Bridge Path ──────────────────────────────────────────────────────

// sendViaBridge sends a request using the bridge for transformation.
func (p *AnthropicOAuthProvider) sendViaBridge(ctx context.Context, req *ProxyRequest, streaming bool) (*ProxyResponse, error) {
	p.logger.Debug("using bridge for transformation")

	// 1. Translate OpenAI → Anthropic format.
	var openaiReq adapter.OpenAIRequest
	if err := json.Unmarshal(req.RawBody, &openaiReq); err != nil {
		return nil, fmt.Errorf("parse openai request: %w", err)
	}

	anthropicReq := adapter.ConvertOpenAIToAnthropic(openaiReq)
	anthropicReq.Stream = streaming

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	// 2. Call bridge to transform.
	result, err := p.bridge.TransformAnthropic(string(body))
	if err != nil {
		return nil, fmt.Errorf("bridge transform: %w", err)
	}

	// 3. Build HTTP request with bridge-provided URL.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, result.URL,
		strings.NewReader(result.Body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// 4. Set headers from bridge — use them directly, don't merge.
	for key, value := range result.Headers {
		httpReq.Header.Set(key, value)
	}

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

// sendStreamViaBridge sends a streaming request using the bridge for transformation.
func (p *AnthropicOAuthProvider) sendStreamViaBridge(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
	p.logger.Debug("using bridge for streaming transformation")

	// 1. Translate OpenAI → Anthropic format.
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

	// 2. Call bridge to transform.
	result, err := p.bridge.TransformAnthropic(string(body))
	if err != nil {
		return nil, fmt.Errorf("bridge transform: %w", err)
	}

	// 3. Build HTTP request with bridge-provided URL.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, result.URL,
		strings.NewReader(result.Body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// 4. Set headers from bridge.
	for key, value := range result.Headers {
		httpReq.Header.Set(key, value)
	}

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

	// 6. Wrap with Go-side tool prefix stripping (NOT via bridge — too slow per-chunk).
	return stream.NewSSEParserWithTransform(resp.Body, transform.StripToolPrefix), nil
}

// ─── Local (Go) Path ──────────────────────────────────────────────────

// sendLocal sends a non-streaming request using the Go-based transformation.
func (p *AnthropicOAuthProvider) sendLocal(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
	p.logger.Debug("using local transformation (bridge unavailable)")

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

// sendStreamLocal sends a streaming request using the Go-based transformation.
func (p *AnthropicOAuthProvider) sendStreamLocal(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
	p.logger.Debug("using local transformation for stream (bridge unavailable)")

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

// ─── Shared Methods ───────────────────────────────────────────────────

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
