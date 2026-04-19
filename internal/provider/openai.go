package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

// ProviderError wraps an HTTP error response from a provider.
// It carries the status code, response headers, and body for classification
// by the fallback chain.
type ProviderError struct {
	ProviderID string
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s returned status %d", e.ProviderID, e.StatusCode)
}

// OpenAIProvider implements the Provider interface for OpenAI API.
// Since OpenAI uses the OpenAI-compatible format natively, this provider
// does a direct forward — no format translation needed.
type OpenAIProvider struct {
	id      string
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *slog.Logger
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(baseURL, apiKey string, models []string, logger *slog.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		id:      "openai",
		name:    "OpenAI",
		baseURL: baseURL,
		apiKey:  apiKey,
		models:  models,
		client:  &http.Client{},
		logger:  logger,
	}
}

// ID returns "openai".
func (p *OpenAIProvider) ID() string { return p.id }

// Name returns "OpenAI".
func (p *OpenAIProvider) Name() string { return p.name }

// BaseURL returns the configured base URL.
func (p *OpenAIProvider) BaseURL() string { return p.baseURL }

// IsAvailable reports whether the provider has an API key configured.
func (p *OpenAIProvider) IsAvailable() bool { return p.apiKey != "" }

// SupportsModel reports whether this provider supports the given model ID.
func (p *OpenAIProvider) SupportsModel(modelID string) bool {
	for _, m := range p.models {
		if m == modelID {
			return true
		}
	}
	return false
}

// ClassifyError classifies an HTTP error response from OpenAI.
func (p *OpenAIProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	return ClassifyOpenAIError(statusCode, headers, body)
}

// Send sends a non-streaming request to OpenAI and returns the complete
// response. Returns a *ProviderError if the response status is not 2xx.
func (p *OpenAIProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
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
			ProviderID: p.id,
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

// SendStream sends a streaming request to OpenAI and returns an SSEParser.
// The caller is responsible for closing the parser.
// Returns a *ProviderError if the response status is not 2xx.
func (p *OpenAIProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
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
			ProviderID: p.id,
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return stream.NewSSEParser(resp.Body), nil
}

// buildRequest creates the HTTP request for the OpenAI API.
func (p *OpenAIProvider) buildRequest(ctx context.Context, req *ProxyRequest) (*http.Request, error) {
	url := p.baseURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.RawBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	return httpReq, nil
}
