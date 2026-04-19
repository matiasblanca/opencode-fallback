package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// --------------------------------------------------------------------------
// New — creates logger at correct level
// --------------------------------------------------------------------------

func TestNewDefaultLevel(t *testing.T) {
	logger := New("info", nil)
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("INFO not enabled at info level")
	}
}

func TestNewDebugLevel(t *testing.T) {
	logger := New("debug", nil)
	if !logger.Enabled(nil, slog.LevelDebug) {
		t.Error("DEBUG not enabled at debug level")
	}
}

func TestNewWarnLevel(t *testing.T) {
	logger := New("warn", nil)
	if !logger.Enabled(nil, slog.LevelWarn) {
		t.Error("WARN not enabled at warn level")
	}
	if logger.Enabled(nil, slog.LevelInfo) {
		t.Error("INFO should not be enabled at warn level")
	}
}

func TestNewErrorLevel(t *testing.T) {
	logger := New("error", nil)
	if !logger.Enabled(nil, slog.LevelError) {
		t.Error("ERROR not enabled at error level")
	}
	if logger.Enabled(nil, slog.LevelWarn) {
		t.Error("WARN should not be enabled at error level")
	}
}

func TestNewTraceLevel(t *testing.T) {
	logger := New("trace", nil)
	// TRACE is below DEBUG (LevelDebug - 4)
	if !logger.Enabled(nil, LevelTrace) {
		t.Error("TRACE not enabled at trace level")
	}
	if !logger.Enabled(nil, slog.LevelDebug) {
		t.Error("DEBUG not enabled at trace level")
	}
}

func TestNewUnknownLevelDefaultsToInfo(t *testing.T) {
	logger := New("garbage", nil)
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("INFO not enabled for unknown level string")
	}
	if logger.Enabled(nil, slog.LevelDebug) {
		t.Error("DEBUG should not be enabled for unknown level (defaults to info)")
	}
}

func TestNewEmptyLevelDefaultsToInfo(t *testing.T) {
	logger := New("", nil)
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("INFO not enabled for empty level string")
	}
}

// --------------------------------------------------------------------------
// ParseLevel
// --------------------------------------------------------------------------

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"trace", LevelTrace},
		{"TRACE", LevelTrace},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// RedactAPIKey
// --------------------------------------------------------------------------

func TestRedactAPIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"anthropic key", "sk-ant-api03-abcdefghijklmnop", "sk-a****mnop"},
		{"openai key", "sk-proj-abcdefghijklmnop", "sk-p****mnop"},
		{"short key", "sk-ab", "****"},
		{"very short", "abc", "****"},
		{"empty", "", "****"},
		{"env ref", "$ANTHROPIC_API_KEY", "$ANTHROPIC_API_KEY"},
		{"eight char key", "12345678", "1234****5678"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactAPIKey(tt.input)
			if got != tt.want {
				t.Errorf("RedactAPIKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Redacting handler — integration test
// --------------------------------------------------------------------------

func TestRedactingHandlerRedactsAPIKey(t *testing.T) {
	var buf bytes.Buffer
	logger := New("debug", &buf)

	logger.Info("testing",
		"api_key", "sk-ant-api03-abcdefghijklmnop",
		"provider", "anthropic",
	)

	output := buf.String()

	// The raw key must not appear in the output.
	if strings.Contains(output, "sk-ant-api03-abcdefghijklmnop") {
		t.Errorf("log output contains raw API key:\n%s", output)
	}

	// The redacted form should appear.
	if !strings.Contains(output, "sk-a****mnop") {
		t.Errorf("log output does not contain redacted key:\n%s", output)
	}

	// Non-sensitive fields should still appear.
	if !strings.Contains(output, "anthropic") {
		t.Errorf("log output does not contain provider:\n%s", output)
	}
}

func TestRedactingHandlerRedactsOAuthToken(t *testing.T) {
	var buf bytes.Buffer
	logger := New("debug", &buf)

	logger.Info("testing",
		"oauth_token", "gho_abcdefghij1234567890",
	)

	output := buf.String()
	if strings.Contains(output, "gho_abcdefghij1234567890") {
		t.Errorf("log output contains raw OAuth token:\n%s", output)
	}
}

func TestRedactingHandlerPassesThroughNormalFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New("debug", &buf)

	logger.Info("request completed",
		"provider", "openai",
		"model", "gpt-4o",
		"duration", "1.23s",
	)

	output := buf.String()
	if !strings.Contains(output, "openai") {
		t.Errorf("missing 'openai' in output:\n%s", output)
	}
	if !strings.Contains(output, "gpt-4o") {
		t.Errorf("missing 'gpt-4o' in output:\n%s", output)
	}
}
