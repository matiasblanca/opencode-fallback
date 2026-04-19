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

// DeepSeekProvider implements the Provider interface for the DeepSeek API.
// DeepSeek uses an OpenAI-compatible API, so this provider is a thin wrapper
// around the same HTTP forwarding logic as OpenAIProvider with DeepSeek-specific
// error classification.
type DeepSeekProvider struct {
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *slog.Logger
}

// NewDeepSeekProvider creates a new DeepSeek provider.
func NewDeepSeekProvider(baseURL, apiKey string, models []string, logger *slog.Logger) *DeepSeekProvider {
	return &DeepSeekProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		models:  models,
		client:  &http.Client{},
		logger:  logger,
	}
}

// ID returns "deepseek".
func (p *DeepSeekProvider) ID() string { return "deepseek" }

// Name returns "DeepSeek".
func (p *DeepSeekProvider) Name() string { return "DeepSeek" }

// BaseURL returns the configured base URL.
func (p *DeepSeekProvider) BaseURL() string { return p.baseURL }

// IsAvailable reports whether the provider has an API key configured.
func (p *DeepSeekProvider) IsAvailable() bool { return p.apiKey != "" }

// SupportsModel reports whether this provider supports the given model ID.
func (p *DeepSeekProvider) SupportsModel(modelID string) bool {
	for _, m := range p.models {
		if m == modelID {
			return true
		}
	}
	return false
}

// ClassifyError classifies an HTTP error response from DeepSeek.
func (p *DeepSeekProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	return ClassifyDeepSeekError(statusCode, headers, body)
}

// Send sends a non-streaming request to DeepSeek.
func (p *DeepSeekProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
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
			ProviderID: "deepseek",
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

// SendStream sends a streaming request to DeepSeek.
func (p *DeepSeekProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
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
			ProviderID: "deepseek",
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return stream.NewSSEParser(resp.Body), nil
}

// buildRequest creates the HTTP request for the DeepSeek API.
func (p *DeepSeekProvider) buildRequest(ctx context.Context, req *ProxyRequest) (*http.Request, error) {
	url := p.baseURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.RawBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	return httpReq, nil
}
