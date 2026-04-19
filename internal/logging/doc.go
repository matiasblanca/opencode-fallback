// Package logging sets up structured logging with log/slog for
// opencode-fallback.
//
// Features:
//   - Three log levels: INFO (fallbacks), DEBUG (requests), TRACE (SSE chunks)
//   - API key redaction: keys never appear in log output
//   - Structured fields for provider, model, duration, reason
//
// This package is at the bottom of the dependency hierarchy alongside config/.
package logging
