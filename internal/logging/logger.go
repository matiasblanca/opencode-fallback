package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// LevelTrace is a custom log level below DEBUG for SSE chunk-level tracing.
// Architecture doc §17: TRACE for SSE chunks.
const LevelTrace = slog.LevelDebug - 4

// sensitiveKeys are log attribute keys whose values must be redacted.
var sensitiveKeys = map[string]bool{
	"api_key":     true,
	"apikey":      true,
	"api-key":     true,
	"oauth_token": true,
	"token":       true,
	"secret":      true,
	"password":    true,
	"authorization": true,
}

// New creates a configured slog.Logger for opencode-fallback.
//
// The level parameter is parsed with ParseLevel (supports "trace", "debug",
// "info", "warn", "error"). If w is nil, output goes to os.Stderr.
//
// API keys and other sensitive values are automatically redacted in log
// output via a wrapping handler.
func New(level string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	lvl := ParseLevel(level)

	inner := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Redact sensitive attributes.
			if isSensitiveKey(a.Key) {
				val := a.Value.String()
				a.Value = slog.StringValue(RedactAPIKey(val))
			}
			return a
		},
	})

	return slog.New(inner)
}

// ParseLevel converts a level string to a slog.Level.
//
// Supported values (case-insensitive): "trace", "debug", "info", "warn",
// "warning", "error". Unknown values default to INFO.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RedactAPIKey masks an API key for safe logging.
//
// Rules:
//   - Environment variable references ("$FOO") are returned as-is
//   - Keys shorter than 8 characters are fully masked as "****"
//   - Longer keys show first 4 and last 4 characters: "sk-an****mnop"
func RedactAPIKey(key string) string {
	// Env var references are safe to log.
	if strings.HasPrefix(key, "$") {
		return key
	}

	if len(key) < 8 {
		return "****"
	}

	return key[:4] + "****" + key[len(key)-4:]
}

// isSensitiveKey reports whether the given attribute key should have its
// value redacted in log output.
func isSensitiveKey(key string) bool {
	return sensitiveKeys[strings.ToLower(key)]
}

// --------------------------------------------------------------------------
// Discard logger (for tests in other packages)
// --------------------------------------------------------------------------

// Discard returns a logger that discards all output. Useful for tests in
// other packages that need a logger but don't want log noise.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --------------------------------------------------------------------------
// NullHandler — minimal handler for when we need context-aware silence
// --------------------------------------------------------------------------

// NullHandler is a slog.Handler that discards all records.
type NullHandler struct{}

// Enabled always returns false.
func (NullHandler) Enabled(context.Context, slog.Level) bool { return false }

// Handle does nothing.
func (NullHandler) Handle(context.Context, slog.Record) error { return nil }

// WithAttrs returns the same handler.
func (h NullHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup returns the same handler.
func (h NullHandler) WithGroup(string) slog.Handler { return h }
