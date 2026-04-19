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
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

const anthropicAPIVersion = "2023-06-01"

// AnthropicProvider implements the Provider interface for the Anthropic
// Messages API. It translates OpenAI-compatible requests to Anthropic format
// using the adapter package, and translates responses back.
type AnthropicProvider struct {
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *slog.Logger
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(baseURL, apiKey string, models []string, logger *slog.Logger) *AnthropicProvider {
	return &AnthropicProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		models:  models,
		client:  &http.Client{},
		logger:  logger,
	}
}

// ID returns "anthropic".
func (p *AnthropicProvider) ID() string { return "anthropic" }

// Name returns "Anthropic".
func (p *AnthropicProvider) Name() string { return "Anthropic" }

// BaseURL returns the configured base URL.
func (p *AnthropicProvider) BaseURL() string { return p.baseURL }

// IsAvailable reports whether the provider has an API key configured.
func (p *AnthropicProvider) IsAvailable() bool { return p.apiKey != "" }

// SupportsModel reports whether this provider supports the given model ID.
func (p *AnthropicProvider) SupportsModel(modelID string) bool {
	for _, m := range p.models {
		if m == modelID {
			return true
		}
	}
	return false
}

// ClassifyError classifies an HTTP error response from Anthropic.
func (p *AnthropicProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	return ClassifyAnthropicError(statusCode, headers, body)
}

// Send sends a non-streaming request to Anthropic.
// It translates the OpenAI request to Anthropic format, sends it, and
// translates the response back to OpenAI format.
func (p *AnthropicProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
	// Parse the OpenAI request from RawBody.
	var openaiReq adapter.OpenAIRequest
	if err := json.Unmarshal(req.RawBody, &openaiReq); err != nil {
		return nil, fmt.Errorf("parse openai request: %w", err)
	}

	// Translate to Anthropic format.
	anthropicReq := adapter.ConvertOpenAIToAnthropic(openaiReq)
	anthropicReq.Stream = false

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	p.setHeaders(httpReq)

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
			ProviderID: "anthropic",
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       respBody,
		}
	}

	// Translate Anthropic response → OpenAI format.
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

// SendStream sends a streaming request to Anthropic.
// For v0.1, the raw Anthropic SSE events are forwarded through the parser.
// Full Anthropic→OpenAI SSE translation is planned for v0.2.
func (p *AnthropicProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
	// Parse the OpenAI request from RawBody.
	var openaiReq adapter.OpenAIRequest
	if err := json.Unmarshal(req.RawBody, &openaiReq); err != nil {
		return nil, fmt.Errorf("parse openai request: %w", err)
	}

	// Translate to Anthropic format with streaming enabled.
	anthropicReq := adapter.ConvertOpenAIToAnthropic(openaiReq)
	anthropicReq.Stream = true

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &ProviderError{
			ProviderID: "anthropic",
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       respBody,
		}
	}

	return stream.NewSSEParser(resp.Body), nil
}

// setHeaders sets the Anthropic-specific request headers.
func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
}
