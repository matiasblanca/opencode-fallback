package screens

import (
	"strings"
	"testing"
)

func TestRenderProviders_MixedAvailability(t *testing.T) {
	providers := []ProviderInfo{
		{ID: "anthropic", DisplayName: "Anthropic", BaseURL: "https://api.anthropic.com", Available: true, Models: []string{"claude-sonnet-4", "claude-haiku-3", "claude-opus-4"}},
		{ID: "openai", DisplayName: "OpenAI", BaseURL: "https://api.openai.com", Available: true, Models: []string{"gpt-4o", "gpt-4o-mini", "o1", "o3-mini"}},
		{ID: "ollama", DisplayName: "Ollama", BaseURL: "http://localhost:11434", Available: false, Models: []string{"llama3", "mistral", "gemma2"}},
	}

	output := RenderProviders(providers, 0, 100)

	if !strings.Contains(output, "Detected Providers") {
		t.Error("should contain title")
	}
	if !strings.Contains(output, "Anthropic") {
		t.Error("should contain Anthropic")
	}
	if !strings.Contains(output, "available") {
		t.Error("should show [available]")
	}
	if !strings.Contains(output, "offline") {
		t.Error("should show [offline]")
	}
	if !strings.Contains(output, "3 models") {
		t.Error("should show model count")
	}
	if !strings.Contains(output, "4 models") {
		t.Error("should show model count for OpenAI")
	}
}

func TestRenderProviders_Empty(t *testing.T) {
	output := RenderProviders(nil, 0, 80)
	if !strings.Contains(output, "No providers") {
		t.Error("should show 'No providers' message when empty")
	}
}

func TestRenderProviders_CursorRendering(t *testing.T) {
	providers := []ProviderInfo{
		{ID: "anthropic", Available: true, Models: []string{"claude-4"}},
		{ID: "openai", Available: true, Models: []string{"gpt-4o"}},
	}

	output := RenderProviders(providers, 1, 80)
	if !strings.Contains(output, ">") {
		t.Error("should contain cursor marker")
	}
}
