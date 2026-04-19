package screens

import (
	"strings"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/config"
)

func TestRenderGlobal_ThreeEntries(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
		{Provider: "deepseek", Model: "deepseek-chat"},
	}

	output := RenderGlobal(chain, 0, 80)

	// Should show 3 models + 1 empty.
	if !strings.Contains(output, "anthropic/claude-sonnet-4") {
		t.Error("should contain anthropic/claude-sonnet-4")
	}
	if !strings.Contains(output, "openai/gpt-4o") {
		t.Error("should contain openai/gpt-4o")
	}
	if !strings.Contains(output, "deepseek/deepseek-chat") {
		t.Error("should contain deepseek/deepseek-chat")
	}
	if !strings.Contains(output, "empty") {
		t.Error("should contain empty slot")
	}
}

func TestRenderGlobal_EmptyChain(t *testing.T) {
	output := RenderGlobal(nil, 0, 80)

	// All 4 slots should show "empty" + the chain summary also says "(empty)".
	// So we expect at least 4 occurrences of "empty" in the slot lines.
	count := strings.Count(output, "empty")
	if count < 4 {
		t.Errorf("expected at least 4 empty references, got %d", count)
	}
}

func TestRenderGlobal_FullChain(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
		{Provider: "deepseek", Model: "deepseek-chat"},
		{Provider: "mistral", Model: "codestral-latest"},
	}

	output := RenderGlobal(chain, 0, 80)

	// Should NOT contain "empty".
	if strings.Contains(output, "empty") {
		t.Error("full chain should not contain empty")
	}
	if !strings.Contains(output, "mistral/codestral-latest") {
		t.Error("should contain mistral/codestral-latest")
	}
}

func TestRenderGlobal_CursorMarker(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "openai", Model: "gpt-4o"},
	}

	output := RenderGlobal(chain, 1, 80)

	// Cursor should be on slot 1 (Fallback 1).
	if !strings.Contains(output, ">") {
		t.Error("should contain cursor marker '>'")
	}
}

func TestRenderGlobal_WithProviders_ShowsStatus(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "ollama", Model: "llama3"},
	}
	providers := []ProviderInfo{
		{ID: "anthropic", Available: true},
		{ID: "ollama", Available: false},
	}

	output := RenderGlobal(chain, 0, 80, providers)

	if !strings.Contains(output, "available") {
		t.Error("should show [available] for anthropic")
	}
	if !strings.Contains(output, "offline") {
		t.Error("should show [offline] for ollama")
	}
}

func TestRenderGlobal_WithUnknownProvider(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "custom-provider", Model: "some-model"},
	}
	providers := []ProviderInfo{
		{ID: "anthropic", Available: true},
	}

	output := RenderGlobal(chain, 0, 80, providers)

	if !strings.Contains(output, "unknown") {
		t.Error("should show [unknown] for provider not in list")
	}
}

func TestRenderGlobal_NarrowWidth(t *testing.T) {
	chain := []config.ChainEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
	}

	// Should not crash at narrow width.
	output := RenderGlobal(chain, 0, 50)
	if !strings.Contains(output, "anthropic/claude-sonnet-4") {
		t.Error("should contain model even at narrow width")
	}
}

func TestRenderGlobal_ShowsHeading(t *testing.T) {
	output := RenderGlobal(nil, 0, 80)

	if !strings.Contains(output, "Global Fallback Chain") {
		t.Error("should contain heading")
	}
	if !strings.Contains(output, "defaults") {
		t.Error("should contain description text")
	}
}
