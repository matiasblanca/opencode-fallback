package screens

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// RenderModelPicker renders the model selection modal.
func RenderModelPicker(providers []ProviderInfo, filteredModels []string,
	filter string, cursor int, width int, height int) string {

	var b strings.Builder

	b.WriteString(styles.Heading.Render("Select Model") + "\n\n")

	// Filter input.
	filterDisplay := filter
	if filterDisplay == "" {
		filterDisplay = styles.Subtle.Render("(type to filter)")
	}
	b.WriteString(fmt.Sprintf("Filter: %s_\n\n", filterDisplay))

	// Model list.
	if len(filteredModels) == 0 {
		if filter != "" {
			b.WriteString(styles.Subtle.Render("No models match the filter.") + "\n")
			b.WriteString(styles.Subtle.Render("Press Enter to use '"+filter+"' as a custom model.") + "\n")
		} else {
			b.WriteString(styles.Subtle.Render("No models available.") + "\n")
		}
	} else {
		// Group by provider.
		currentProvider := ""
		maxVisible := height - 12
		if maxVisible < 5 {
			maxVisible = 20
		}

		displayed := 0
		for i, model := range filteredModels {
			if displayed >= maxVisible {
				remaining := len(filteredModels) - displayed
				b.WriteString(styles.Subtle.Render(fmt.Sprintf("  ... and %d more\n", remaining)))
				break
			}

			// Extract provider prefix.
			parts := strings.SplitN(model, "/", 2)
			providerName := parts[0]

			// Show provider header when it changes.
			if providerName != currentProvider {
				if currentProvider != "" {
					b.WriteString("\n")
				}
				currentProvider = providerName
				providerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ProviderColor(providerName))
				b.WriteString("  " + providerStyle.Render(providerName) + "\n")
			}

			// Cursor.
			cursorChar := "    "
			if i == cursor {
				cursorChar = styles.Cursor.Render("  > ")
			}

			// Find availability.
			available := false
			for _, p := range providers {
				if p.ID == providerName {
					available = p.Available
					break
				}
			}

			statusStr := styles.AvailableStatus.Render("[available]")
			if !available {
				statusStr = styles.OfflineStatus.Render("[offline]")
			}

			b.WriteString(fmt.Sprintf("%s%-38s %s\n", cursorChar, model, statusStr))
			displayed++
		}
	}

	b.WriteString("\n" + styles.Subtle.Render("Type to filter | Enter: select | Esc: cancel"))

	return styles.SlotBox.Width(min(width-4, 70)).Render(b.String())
}
