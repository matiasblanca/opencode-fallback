package provider

import "log/slog"

// NewDeepSeekProvider creates a GenericOpenAIProvider pre-configured for the
// DeepSeek API. Kept for backward compatibility.
func NewDeepSeekProvider(baseURL, apiKey string, models []string, logger *slog.Logger) *GenericOpenAIProvider {
	return NewGenericOpenAIProvider(
		"deepseek", "DeepSeek",
		baseURL, apiKey,
		AuthTypeBearer, models,
		nil, // uses ClassifyGenericOpenAIError (superset of old ClassifyDeepSeekError)
		logger,
	)
}
