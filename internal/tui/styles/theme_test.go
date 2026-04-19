package styles

import (
	"fmt"
	"image/color"
	"testing"
)

func colorKey(c color.Color) string {
	r, g, b, a := c.RGBA()
	return fmt.Sprintf("%d-%d-%d-%d", r, g, b, a)
}

func TestProviderColor_KnownProviders(t *testing.T) {
	providers := []string{"anthropic", "openai", "deepseek", "mistral", "gemini", "openrouter", "ollama"}
	colors := make(map[string]string)

	for _, p := range providers {
		c := ProviderColor(p)
		key := colorKey(c)
		if prev, exists := colors[key]; exists {
			t.Errorf("providers %q and %q have the same color", prev, p)
		}
		colors[key] = p
	}
}

func TestProviderColor_UnknownProvider(t *testing.T) {
	c := ProviderColor("some-unknown-provider")
	expected := ProviderColor("another-unknown")
	if colorKey(c) != colorKey(expected) {
		t.Errorf("unknown providers should return the same default color")
	}
}

func TestProviderColor_DefaultDistinctFromKnown(t *testing.T) {
	defaultColor := colorKey(ProviderColor("unknown"))
	knownProviders := []string{"anthropic", "openai", "deepseek", "mistral", "gemini", "openrouter"}
	for _, p := range knownProviders {
		if colorKey(ProviderColor(p)) == defaultColor {
			t.Errorf("ProviderColor(%q) should differ from default color", p)
		}
	}
}
