package screens

import (
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/config"
)

func TestTruncate_ASCII(t *testing.T) {
	result := Truncate("hello world", 5)
	if result != "he..." {
		t.Errorf("Truncate ASCII = %q, want %q", result, "he...")
	}
}

func TestTruncate_ASCII_NoTruncation(t *testing.T) {
	result := Truncate("hello", 10)
	if result != "hello" {
		t.Errorf("Truncate no-trunc = %q, want %q", result, "hello")
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	result := Truncate("hello", 5)
	if result != "hello" {
		t.Errorf("Truncate exact = %q, want %q", result, "hello")
	}
}

func TestTruncate_Unicode_Emojis(t *testing.T) {
	// "🎉🎊🎈🎁🎂" = 5 runes but 20 bytes.
	input := "🎉🎊🎈🎁🎂"
	result := Truncate(input, 4)
	// Should truncate to 1 rune + "..." = "🎉..."
	if result != "🎉..." {
		t.Errorf("Truncate unicode = %q, want %q", result, "🎉...")
	}
}

func TestTruncate_Unicode_NoCrash(t *testing.T) {
	// Ensure we don't split multi-byte chars.
	input := "café"
	result := Truncate(input, 3)
	if len([]rune(result)) > 3 {
		t.Errorf("Truncate should limit to 3 runes, got %d", len([]rune(result)))
	}
}

func TestTruncate_ShortMaxLen(t *testing.T) {
	result := Truncate("hello", 2)
	if result != "he" {
		t.Errorf("Truncate short = %q, want %q", result, "he")
	}
}

func TestFormatChainSummary_Basic(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude"},
		{Provider: "openai", Model: "gpt-4o"},
	}
	result := FormatChainSummary(chain)
	if result != "anthropic/claude → openai/gpt-4o" {
		t.Errorf("FormatChainSummary = %q, unexpected", result)
	}
}

func TestFormatChainSummary_Empty(t *testing.T) {
	result := FormatChainSummary(nil)
	if result != "(empty)" {
		t.Errorf("FormatChainSummary empty = %q, want '(empty)'", result)
	}
}

func TestFormatChainSummary_WithMaxLen(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
		{Provider: "deepseek", Model: "deepseek-chat"},
		{Provider: "mistral", Model: "codestral-latest"},
	}
	result := FormatChainSummary(chain, 30)
	if len([]rune(result)) > 30 {
		t.Errorf("FormatChainSummary with maxLen should be <= 30 runes, got %d", len([]rune(result)))
	}
}

func TestRenderHelp_MainGlobal(t *testing.T) {
	output := RenderHelp(HelpScreenMain, 0, 80, 24)
	if !contains(output, "Edit slot") {
		t.Error("Global tab help should mention 'Edit slot'")
	}
	if !contains(output, "Clear slot") {
		t.Error("Global tab help should mention 'Clear slot'")
	}
	if contains(output, "Add agent") {
		t.Error("Global tab help should NOT mention 'Add agent'")
	}
}

func TestRenderHelp_MainAgents(t *testing.T) {
	output := RenderHelp(HelpScreenMain, 1, 80, 24)
	if !contains(output, "Add agent") {
		t.Error("Agents tab help should mention 'Add agent'")
	}
	if !contains(output, "Edit agent") {
		t.Error("Agents tab help should mention 'Edit agent'")
	}
}

func TestRenderHelp_ChainEditor(t *testing.T) {
	output := RenderHelp(HelpScreenChainEditor, 0, 80, 24)
	if !contains(output, "Delete override") {
		t.Error("ChainEditor help should mention 'Delete override'")
	}
	if !contains(output, "Create override") {
		t.Error("ChainEditor help should mention 'Create override'")
	}
	// Should NOT contain Tab or q.
	if contains(output, "Switch tab") {
		t.Error("ChainEditor help should NOT mention 'Switch tab'")
	}
	if contains(output, "Quit") {
		t.Error("ChainEditor help should NOT mention 'Quit'")
	}
}

func TestRenderHelp_ModelPicker(t *testing.T) {
	output := RenderHelp(HelpScreenModelPicker, 0, 80, 24)
	if !contains(output, "Filter") {
		t.Error("ModelPicker help should mention filtering")
	}
	if !contains(output, "Backspace") {
		t.Error("ModelPicker help should mention 'Backspace'")
	}
}

func TestRenderHelp_Providers(t *testing.T) {
	output := RenderHelp(HelpScreenProviders, 0, 80, 24)
	if !contains(output, "Go back") {
		t.Error("Providers help should mention 'Go back'")
	}
}

func TestRenderScrollIndicator_AtStart(t *testing.T) {
	result := RenderScrollIndicator(0, 20, 10)
	if !contains(result, "1/20") {
		t.Errorf("should contain position [1/20], got %q", result)
	}
	if !contains(result, "▼") {
		t.Error("should contain down arrow at start")
	}
	if contains(result, "▲") {
		t.Error("should NOT contain up arrow at start")
	}
}

func TestRenderScrollIndicator_InMiddle(t *testing.T) {
	result := RenderScrollIndicator(5, 20, 10)
	if !contains(result, "6/20") {
		t.Errorf("should contain position [6/20], got %q", result)
	}
	if !contains(result, "▲") {
		t.Error("should contain up arrow in middle")
	}
	if !contains(result, "▼") {
		t.Error("should contain down arrow in middle")
	}
}

func TestRenderScrollIndicator_AtEnd(t *testing.T) {
	result := RenderScrollIndicator(10, 20, 10)
	if !contains(result, "11/20") {
		t.Errorf("should contain position [11/20], got %q", result)
	}
	if !contains(result, "▲") {
		t.Error("should contain up arrow at end")
	}
	if contains(result, "▼") {
		t.Error("should NOT contain down arrow at end")
	}
}

func TestRenderScrollIndicator_NoItems(t *testing.T) {
	result := RenderScrollIndicator(0, 0, 10)
	if result != "" {
		t.Errorf("should return empty for 0 items, got %q", result)
	}
}

func TestRenderAddAgentInput(t *testing.T) {
	output := RenderAddAgentInput("my-agent", 80)
	if output == "" {
		t.Error("should not be empty")
	}
	if !contains(output, "my-agent") {
		t.Error("should contain the input text")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
