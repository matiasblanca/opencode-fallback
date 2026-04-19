package config

import (
	"net/http"
	"os"
	"time"
)

// ollamaTagsURL is the endpoint used to detect a running Ollama instance.
const ollamaTagsURL = "http://localhost:11434/api/tags"

// anthropicModels is the list of standard Anthropic models added when the
// ANTHROPIC_API_KEY environment variable is present.
var anthropicModels = []string{
	"claude-opus-4-5",
	"claude-sonnet-4-5",
	"claude-haiku-3-5",
}

// openaiModels is the list of standard OpenAI models added when the
// OPENAI_API_KEY environment variable is present.
var openaiModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"o1",
	"o3-mini",
}

// deepseekModels is the list of standard DeepSeek models added when the
// DEEPSEEK_API_KEY environment variable is present.
var deepseekModels = []string{
	"deepseek-chat",
	"deepseek-reasoner",
}

// ollamaModels is the default list of Ollama models when a local instance
// is detected. Users can override this list in config.json.
var ollamaModels = []string{
	"llama3",
	"mistral",
	"gemma2",
}

// mistralModels is the list of standard Mistral models added when the
// MISTRAL_API_KEY environment variable is present.
var mistralModels = []string{
	"mistral-large-latest",
	"codestral-latest",
}

// geminiModels is the list of standard Gemini models added when the
// GEMINI_API_KEY environment variable is present.
var geminiModels = []string{
	"gemini-2.5-pro",
	"gemini-2.5-flash",
}

// providerOrder defines the precedence order for the global fallback chain.
// Providers detected later in this list are added after earlier ones.
var providerOrder = []string{"anthropic", "openai", "deepseek", "mistral", "gemini", "openrouter", "ollama"}

// DetectAvailableProviders auto-detects LLM providers from the environment.
//
// It checks well-known environment variables for API keys and, for Ollama,
// attempts a lightweight HTTP probe to http://localhost:11434/api/tags with a
// 2-second timeout.
//
// Returns:
//   - providers: map of provider name → ProviderConfig for each detected provider
//   - chain: global fallback chain ordered anthropic → openai → deepseek → ollama
func DetectAvailableProviders() (providers map[string]ProviderConfig, chain []ChainEntry) {
	providers = make(map[string]ProviderConfig)

	// Anthropic
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		providers["anthropic"] = ProviderConfig{
			BaseURL: "https://api.anthropic.com",
			APIKey:  key,
			Models:  anthropicModels,
		}
	}

	// OpenAI
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers["openai"] = ProviderConfig{
			BaseURL: "https://api.openai.com",
			APIKey:  key,
			Models:  openaiModels,
		}
	}

	// DeepSeek
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		providers["deepseek"] = ProviderConfig{
			BaseURL: "https://api.deepseek.com",
			APIKey:  key,
			Models:  deepseekModels,
		}
	}

	// Mistral
	if key := os.Getenv("MISTRAL_API_KEY"); key != "" {
		providers["mistral"] = ProviderConfig{
			BaseURL: "https://api.mistral.ai",
			APIKey:  key,
			Models:  mistralModels,
		}
	}

	// Google Gemini (OpenAI-compatible endpoint)
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		providers["gemini"] = ProviderConfig{
			BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
			APIKey:  key,
			Models:  geminiModels,
		}
	}

	// OpenRouter
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		providers["openrouter"] = ProviderConfig{
			BaseURL: "https://openrouter.ai/api/v1",
			APIKey:  key,
			// OpenRouter accepts any model; leave Models empty.
		}
	}

	// Ollama — probe via HTTP
	if probeOllama() {
		providers["ollama"] = ProviderConfig{
			BaseURL:  "http://localhost:11434",
			AuthType: "none",
			Models:   ollamaModels,
		}
	}

	// Build global chain in canonical order
	chain = buildGlobalChain(providers)

	return providers, chain
}

// probeOllama attempts a GET to the Ollama tags endpoint with a short timeout.
// Returns true if Ollama responds with any 2xx status.
func probeOllama() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaTagsURL)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// buildGlobalChain creates a ChainEntry list from detected providers,
// using the first model of each provider, ordered by providerOrder.
func buildGlobalChain(providers map[string]ProviderConfig) []ChainEntry {
	var chain []ChainEntry
	for _, name := range providerOrder {
		p, ok := providers[name]
		if !ok || len(p.Models) == 0 {
			continue
		}
		chain = append(chain, ChainEntry{
			Provider: name,
			Model:    p.Models[0],
		})
	}
	return chain
}
