package provider

import "log/slog"

// NewOllamaProvider creates a GenericOpenAIProvider pre-configured for the
// Ollama local inference server. No API key is needed (AuthTypeNone).
// Kept for backward compatibility.
func NewOllamaProvider(baseURL string, models []string, logger *slog.Logger) *GenericOpenAIProvider {
	return NewGenericOpenAIProvider(
		"ollama", "Ollama",
		baseURL, "",
		AuthTypeNone, models,
		nil, // uses ClassifyGenericOpenAIError
		logger,
	)
}
