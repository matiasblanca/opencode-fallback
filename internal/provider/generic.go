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

// AuthType defines how a provider authenticates requests.
type AuthType string

const (
	// AuthTypeBearer sends an Authorization: Bearer <key> header.
	AuthTypeBearer AuthType = "bearer"
	// AuthTypeNone sends no authentication header (e.g. Ollama).
	AuthTypeNone AuthType = "none"
)

// ErrorClassifier is a function that classifies an HTTP error response from a
// provider into retriable or fatal.
type ErrorClassifier func(statusCode int, headers http.Header, body []byte) ErrorClassification

// GenericOpenAIProvider implements Provider for any OpenAI-compatible API.
// It is configured with an ID, name, base URL, API key, auth type, model list,
// and an optional error classifier. If the classifier is nil, it defaults to
// ClassifyGenericOpenAIError.
type GenericOpenAIProvider struct {
	id         string
	name       string
	baseURL    string
	apiKey     string
	authType   AuthType
	models     []string
	classifier ErrorClassifier
	client     *http.Client
	logger     *slog.Logger
}

// NewGenericOpenAIProvider creates a new GenericOpenAIProvider with the given
// configuration. Pass nil for classifier to use ClassifyGenericOpenAIError.
func NewGenericOpenAIProvider(
	id, name, baseURL, apiKey string,
	authType AuthType,
	models []string,
	classifier ErrorClassifier,
	logger *slog.Logger,
) *GenericOpenAIProvider {
	return &GenericOpenAIProvider{
		id:         id,
		name:       name,
		baseURL:    baseURL,
		apiKey:     apiKey,
		authType:   authType,
		models:     models,
		classifier: classifier,
		client:     &http.Client{},
		logger:     logger,
	}
}

// ID returns the unique provider identifier.
func (p *GenericOpenAIProvider) ID() string { return p.id }

// Name returns the human-readable provider name.
func (p *GenericOpenAIProvider) Name() string { return p.name }

// BaseURL returns the configured base URL.
func (p *GenericOpenAIProvider) BaseURL() string { return p.baseURL }

// IsAvailable reports whether this provider is configured and ready to use.
// For AuthTypeNone (e.g. Ollama), it always returns true.
// For AuthTypeBearer, it returns true only when apiKey is non-empty.
func (p *GenericOpenAIProvider) IsAvailable() bool {
	if p.authType == AuthTypeNone {
		return true
	}
	return p.apiKey != ""
}

// SupportsModel reports whether the given model ID is in the configured list.
func (p *GenericOpenAIProvider) SupportsModel(modelID string) bool {
	for _, m := range p.models {
		if m == modelID {
			return true
		}
	}
	return false
}

// ClassifyError classifies an HTTP error response. Uses the custom classifier
// if one was provided; otherwise falls back to ClassifyGenericOpenAIError.
func (p *GenericOpenAIProvider) ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification {
	if p.classifier != nil {
		return p.classifier(statusCode, headers, body)
	}
	return ClassifyGenericOpenAIError(statusCode, headers, body)
}

// Send sends a non-streaming request to the provider and returns the complete
// response. Returns a *ProviderError if the response status is >= 400.
func (p *GenericOpenAIProvider) Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
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

// SendStream sends a streaming request to the provider and returns an
// SSEParser. The caller is responsible for closing the parser.
// Returns a *ProviderError if the response status is >= 400.
func (p *GenericOpenAIProvider) SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error) {
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

// buildRequest creates the HTTP request for the OpenAI-compatible endpoint.
// Adds Authorization: Bearer header only when authType is AuthTypeBearer.
func (p *GenericOpenAIProvider) buildRequest(ctx context.Context, req *ProxyRequest) (*http.Request, error) {
	url := p.baseURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.RawBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.authType == AuthTypeBearer {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	return httpReq, nil
}
