// Package proxy implements the HTTP server and request handler for the
// OpenAI-compatible proxy.
//
// Dependency rules:
//   - proxy/ imports fallback/, config/, logging/
//   - proxy/ does NOT import provider/ or adapter/ directly
//
// The proxy accepts POST requests to /v1/chat/completions, parses the
// OpenAI-format request body, and dispatches it to the fallback chain.
// Responses are returned in OpenAI format, including SSE streaming.
package proxy
