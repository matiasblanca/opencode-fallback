package provider

import (
	"fmt"
	"log/slog"
	"net/http"
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

// NewOpenAIProvider creates a GenericOpenAIProvider pre-configured for the
// OpenAI API. Kept for backward compatibility.
func NewOpenAIProvider(baseURL, apiKey string, models []string, logger *slog.Logger) *GenericOpenAIProvider {
	return NewGenericOpenAIProvider(
		"openai", "OpenAI",
		baseURL, apiKey,
		AuthTypeBearer, models,
		ClassifyOpenAIError,
		logger,
	)
}
