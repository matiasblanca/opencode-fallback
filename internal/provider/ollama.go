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

// OllamaProvider implements the Provider interface for the Ollama local
// inference server. Ollama exposes an OpenAI-compatible API at
// /v1/chat/completions, so this provider does direct forwarding.
//
// No API key is needed — Ollama runs on localhost with no auth.
type OllamaProvider struct {
	baseURL string
	models  []string
	client  *http.Client
	logger  *slog.Logger
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(baseURL string, models []string, logger *slog.Logger) *OllamaProvider {
	return &OllamaProvider{
		baseURL: baseURL,
		models:  models,
		client:  &http.Client{},
		logger:  logger,
	}
}

// ID returns "ollama".
func (p *OllamaProvider) ID() string { return "ollama" }

// Name returns "Ollama".
func (p *OllamaProvider) Name() string { return "Ollama" }

// BaseURL returns the configured base URL (typically http://localhost:11434).
func (p *OllamaProvider) BaseURL() string { return p.baseURL }

// IsAvailable reports whether Ollama is assumed to be reachable.
// Since Ollama requires no API key, this always returns true. Actual
// availability is checked by the circuit breaker at request time.
func (p *OllamaProvider) IsAvailable() bool { return true }

// SupportsModel reports whether this provider lists the given model ID.
func (p *OllamaProvider) SupportsModel(modelID string) bool {
	for _, m := range p.models {
		if m == modelID {
			return true
		}
	}
	return false
}

// ClassifyError classifies an HTTP error response from Ollama.
// Ollama errors are treated like generic server errors; connection failures
// are caught by the transport error classifier.
func (p *OllamaProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	switch {
	case statusCode == 404:
		// Model not found in Ollama — fatal.
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "model_not_found",
			StatusCode: statusCode,
		}
	case statusCode >= 500:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "server_error",
			StatusCode: statusCode,
		}
	default:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "client_error",
			StatusCode: statusCode,
		}
	}
}

// Send sends a non-streaming request to Ollama.
func (p *OllamaProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
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
			ProviderID: "ollama",
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

// SendStream sends a streaming request to Ollama.
func (p *OllamaProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
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
			ProviderID: "ollama",
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
	}

	return stream.NewSSEParser(resp.Body), nil
}

// buildRequest creates the HTTP request for Ollama. No Authorization header
// is set because Ollama runs locally without authentication.
func (p *OllamaProvider) buildRequest(ctx context.Context, req *ProxyRequest) (*http.Request, error) {
	url := p.baseURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.RawBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	return httpReq, nil
}
