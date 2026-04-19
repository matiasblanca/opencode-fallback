package screens

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// maxSlots is the number of slots in a fallback chain.
const maxSlots = 4

// RenderGlobal renders the global fallback chain editor.
// providers is optional — when provided, shows [available]/[offline] next to each model.
func RenderGlobal(chain []config.ChainEntry, cursor int, width int, providers ...[]ProviderInfo) string {
	var b strings.Builder

	b.WriteString(styles.Heading.Render("Global Fallback Chain") + "\n")
	b.WriteString(styles.Subtle.Render("These defaults apply to all agents without a custom chain.") + "\n\n")

	// Responsive model column width.
	modelColW := min(width-4, 70) - 14
	if modelColW < 20 {
		modelColW = 20
	}

	// Header row.
	b.WriteString(fmt.Sprintf("  %-12s %-*s\n", "Slot", modelColW, "Model"))
	b.WriteString(fmt.Sprintf("  %-12s %-*s\n", strings.Repeat("─", 11), modelColW, strings.Repeat("─", min(modelColW, 38))))

	// Render 4 slots.
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

	// Chain summary.
	b.WriteString("\n" + styles.Subtle.Render("Resolved chain: "+FormatChainSummary(chain)) + "\n")

	return b.String()
}
