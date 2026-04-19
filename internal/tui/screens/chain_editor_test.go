package screens

import (
	"strings"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/config"
)

func TestRenderChainEditor_WithOverride(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "mistral", Model: "codestral-latest"},
		{Provider: "mistral", Model: "devstal-2-latest"},
		{Provider: "openai", Model: "gpt-5.3-instant"},
	}
	global := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
	}

	output := RenderChainEditor("sdd-apply", "anthropic/claude-sonnet-4-6",
		chain, global, true, 0, false, "", 80)

	if !strings.Contains(output, "sdd-apply") {
		t.Error("should contain agent name")
	}
	if !strings.Contains(output, "anthropic/claude-sonnet-4-6") {
		t.Error("should contain current model")
	}
	if !strings.Contains(output, "custom") {
		t.Error("should show [custom] badge for override")
	}
	if !strings.Contains(output, "mistral/codestral-latest") {
		t.Error("should contain override chain entry")
	}
	if !strings.Contains(output, "anthropic/claude-sonnet-4") {
		t.Error("should show global chain as reference")
	}
}

func TestRenderChainEditor_WithoutOverride(t *testing.T) {
	global := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
	}

	output := RenderChainEditor("gentleman", "anthropic/claude-sonnet-4-6",
		global, global, false, 0, false, "", 80)

	if !strings.Contains(output, "uses global") {
		t.Error("should show (uses global) when no override")
	}
	if !strings.Contains(output, "override") {
		t.Error("should mention creating an override with 'o'")
	}
}

func TestRenderChainEditor_ShowsGlobalReference(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "deepseek", Model: "deepseek-chat"},
	}
	global := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
	}

	output := RenderChainEditor("test-agent", "test/model",
		chain, global, true, 0, false, "", 80)

	// Should show global chain as reference.
	if !strings.Contains(output, "Without override") {
		t.Error("should contain 'Without override' label")
	}
	if !strings.Contains(output, "anthropic/claude-sonnet-4") {
		t.Error("should contain global chain entries")
	}
}

func TestRenderChainEditor_WithProviders(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "ollama", Model: "llama3"},
	}
	global := []config.ChainEntry{}
	providers := []ProviderInfo{
		{ID: "anthropic", Available: true},
		{ID: "ollama", Available: false},
	}

	output := RenderChainEditor("test-agent", "test/model",
		chain, global, true, 0, false, "", 80, providers)

	if !strings.Contains(output, "available") {
		t.Error("should show [available] for anthropic")
	}
	if !strings.Contains(output, "offline") {
		t.Error("should show [offline] for ollama")
	}
}

func TestRenderChainEditor_UnknownProvider(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "custom-x", Model: "model-x"},
	}
	providers := []ProviderInfo{
		{ID: "anthropic", Available: true},
	}

	output := RenderChainEditor("test-agent", "",
		chain, nil, true, 0, false, "", 80, providers)

	if !strings.Contains(output, "unknown") {
		t.Error("should show [unknown] for unrecognized provider")
	}
}

func TestRenderChainEditor_UnsavedIndicator(t *testing.T) {
	output := RenderChainEditor("test", "", nil, nil, false, 0, true, "", 80)
	if !strings.Contains(output, "unsaved") {
		t.Error("should show [unsaved] when dirty")
	}
}

func TestRenderChainEditor_StatusMessage(t *testing.T) {
	output := RenderChainEditor("test", "", nil, nil, false, 0, false, "Saved!", 80)
	if !strings.Contains(output, "Saved!") {
		t.Error("should show status message")
	}
}
