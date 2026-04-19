package screens

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// RenderChainEditor renders the 4-slot chain editor for an agent.
// providers is optional — when provided, shows [available]/[offline] next to each model.
func RenderChainEditor(agentName string, currentModel string,
	chain []config.ChainEntry, globalChain []config.ChainEntry,
	hasOverride bool, cursor int, dirty bool, statusMsg string, width int,
	providers ...[]ProviderInfo) string {

	var b strings.Builder

	// Header.
	b.WriteString(styles.Subtle.Render("< Back (Esc)"))
	title := fmt.Sprintf("Agent: %s", agentName)
	pad := width - 12 - len(title) - 4
	if pad < 2 {
		pad = 2
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(styles.Heading.Render(title))

	// Status indicators.
	if dirty {
		b.WriteString("  " + styles.UnsavedBadge.Render("[unsaved]"))
	}
	if statusMsg != "" {
		if strings.HasPrefix(statusMsg, "Error") {
			b.WriteString("  " + styles.Error.Render(statusMsg))
		} else {
			b.WriteString("  " + styles.Success.Render(statusMsg))
		}
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(width, 80)) + "\n\n")

	// Current model info.
	if currentModel != "" {
		b.WriteString(fmt.Sprintf("Current model in OpenCode: %s\n", styles.Heading.Render(currentModel)))
	}

	// Override status.
	if hasOverride {
		b.WriteString("Fallback: " + styles.CustomBadge.Render("[custom]") +
			" override (press 'd' to delete and use global)\n\n")
	} else {
		b.WriteString("Fallback: " + styles.GlobalBadge.Render("(uses global)") +
			" — press 'o' to create custom override\n\n")
	}

	// Slot table with responsive width.
	modelColW := min(width-4, 70) - 14
	if modelColW < 20 {
		modelColW = 20
	}
	b.WriteString(fmt.Sprintf("  %-12s %-*s\n", "Slot", modelColW, "Model"))
	b.WriteString(fmt.Sprintf("  %-12s %-*s\n", strings.Repeat("─", 11), modelColW, strings.Repeat("─", min(modelColW, 38))))

	for i := 0; i < maxSlots; i++ {
		cursorChar := "  "
		if i == cursor {
			cursorChar = styles.Cursor.Render("> ")
		}

		label := SlotLabel(i)
		var model string
		if i < len(chain) && chain[i].Provider != "" {
			modelColor := lipgloss.NewStyle().Foreground(styles.ProviderColor(chain[i].Provider))
			model = modelColor.Render(chain[i].Provider + "/" + chain[i].Model)
			// Show provider status if providers are available.
			if len(providers) > 0 {
				model += "  " + ProviderStatusStr(chain[i].Provider, providers[0])
			}
		} else {
			model = styles.Subtle.Render("(empty — press Enter to add)")
		}

		b.WriteString(fmt.Sprintf("%s%-12s %s\n", cursorChar, label, model))
	}

	// Global chain reference (always show).
	b.WriteString("\n" + styles.Subtle.Render("Without override (global):") + "\n")
	b.WriteString(styles.Subtle.Render(FormatChainSummary(globalChain)) + "\n\n")

	// Footer.
	b.WriteString(styles.Subtle.Render("j/k: navigate  Enter: change model  x: clear  d: delete override  o: create override  Ctrl+S: save  Esc: back"))

	return b.String()
}
