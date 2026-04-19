// Package provider defines the Provider interface and implements concrete
// LLM providers (Anthropic, OpenAI, DeepSeek, Ollama).
//
// Dependency rules:
//   - provider/ imports adapter/, circuit/, stream/, config/
//   - provider/ does NOT import fallback/ or proxy/
//
// Each provider knows how to send requests in its native format and classify
// errors as retriable or fatal. The Provider interface is the central
// contract of the system.
//
// The package also includes a Registry (map[string]Provider) for looking up
// providers by ID, and error classification functions per provider.
package provider
