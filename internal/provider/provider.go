package provider

import (
	"context"
	"net/http"

	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

// Provider is the central contract of the system. Each LLM provider
// implements this interface to send requests and classify errors in its
// native format.
//
// Dependency rules: provider/ imports adapter/, circuit/, stream/, config/.
// provider/ does NOT import fallback/ or proxy/.
type Provider interface {
	// ID returns the unique identifier of the provider (e.g. "anthropic", "openai").
	ID() string

	// Name returns the human-readable name of the provider.
	Name() string

	// BaseURL returns the base URL of the provider API.
	BaseURL() string

	// Send sends a non-streaming request and returns the complete response.
	// The request arrives in OpenAI-compatible format.
	Send(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error)

	// SendStream sends a streaming request and returns an SSEParser that
	// emits SSE events in OpenAI format. The caller is responsible for
	// closing the parser.
	SendStream(ctx context.Context, req *ProxyRequest) (*stream.SSEParser, error)

	// ClassifyError determines whether an HTTP error response is retriable
	// or fatal, based on the status code, headers, and body.
	ClassifyError(statusCode int, headers http.Header, body []byte) ErrorClassification

	// SupportsModel reports whether this provider supports the given model ID.
	SupportsModel(modelID string) bool

	// IsAvailable reports whether the provider has credentials configured.
	IsAvailable() bool
}
