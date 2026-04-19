package screens

import (
	"strings"
	"testing"
)

func testPickerProviders() []ProviderInfo {
	return []ProviderInfo{
		{ID: "anthropic", DisplayName: "Anthropic", Available: true, Models: []string{"claude-sonnet-4", "claude-haiku-3"}},
		{ID: "openai", DisplayName: "OpenAI", Available: true, Models: []string{"gpt-4o", "gpt-4o-mini", "o1"}},
		{ID: "deepseek", DisplayName: "DeepSeek", Available: false, Models: []string{"deepseek-chat", "deepseek-reasoner"}},
	}
}

func TestRenderModelPicker_AllModels(t *testing.T) {
	providers := testPickerProviders()
	allModels := []string{
		"anthropic/claude-haiku-3",
		"anthropic/claude-sonnet-4",
		"deepseek/deepseek-chat",
		"deepseek/deepseek-reasoner",
		"openai/gpt-4o",
		"openai/gpt-4o-mini",
		"openai/o1",
	}

	output := RenderModelPicker(providers, allModels, "", 0, 80, 30)

	if !strings.Contains(output, "Select Model") {
		t.Error("should contain title")
	}
	// Should show all 7 models.
	for _, model := range allModels {
		if !strings.Contains(output, model) {
			t.Errorf("should contain model %q", model)
		}
	}
}

func TestRenderModelPicker_FilterDeep(t *testing.T) {
	providers := testPickerProviders()
	filtered := []string{
		"deepseek/deepseek-chat",
		"deepseek/deepseek-reasoner",
	}

	output := RenderModelPicker(providers, filtered, "deep", 0, 80, 30)

	if !strings.Contains(output, "deepseek/deepseek-chat") {
		t.Error("should contain deepseek-chat")
	}
	// Should NOT contain openai models since they're filtered out.
	if strings.Contains(output, "openai/gpt-4o") {
		t.Error("should NOT contain filtered-out models")
	}
}

func TestRenderModelPicker_EmptyFilter(t *testing.T) {
	providers := testPickerProviders()
	allModels := []string{
		"anthropic/claude-sonnet-4",
		"openai/gpt-4o",
		"deepseek/deepseek-chat",
	}

	output := RenderModelPicker(providers, allModels, "", 0, 80, 30)

	// All models should be visible.
	for _, model := range allModels {
		if !strings.Contains(output, model) {
			t.Errorf("should contain model %q with empty filter", model)
		}
	}
}

func TestRenderModelPicker_AvailabilityStatus(t *testing.T) {
	providers := testPickerProviders()
	allModels := []string{
		"anthropic/claude-sonnet-4",
		"deepseek/deepseek-chat",
	}

	output := RenderModelPicker(providers, allModels, "", 0, 80, 30)

	if !strings.Contains(output, "available") {
		t.Error("should show [available] for available providers")
	}
	if !strings.Contains(output, "offline") {
		t.Error("should show [offline] for unavailable providers")
	}
}

func TestRenderModelPicker_ProviderColorsApplied(t *testing.T) {
	providers := testPickerProviders()
	allModels := []string{
		"anthropic/claude-sonnet-4",
		"openai/gpt-4o",
	}

	output := RenderModelPicker(providers, allModels, "", 0, 80, 30)

	// ANSI escape sequences should be present (colors applied).
	if !strings.Contains(output, "\033[") {
		t.Error("output should contain ANSI escape sequences for provider colors")
	}
}

func TestRenderModelPicker_NoMatches(t *testing.T) {
	providers := testPickerProviders()

	output := RenderModelPicker(providers, nil, "zzzzz", 0, 80, 30)

	if !strings.Contains(output, "No models match") {
		t.Error("should show 'No models match' message")
	}
	if !strings.Contains(output, "zzzzz") {
		t.Error("should show filter text as custom model option")
	}
}
