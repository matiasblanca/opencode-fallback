package screens

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// RenderProviders renders the detected providers list.
func RenderProviders(providers []ProviderInfo, cursor int, width int) string {
	var b strings.Builder

	b.WriteString(styles.Subtle.Render("< Back (Esc)"))
	b.WriteString(strings.Repeat(" ", 10))
	b.WriteString(styles.Heading.Render("Detected Providers") + "\n")
	b.WriteString(strings.Repeat("─", min(width, 80)) + "\n\n")

	if len(providers) == 0 {
		b.WriteString(styles.Subtle.Render("No providers detected.\n"))
		return b.String()
	}

	// Responsive column widths.
	nameW := 16
	urlW := 35
	if width < 80 {
		nameW = 12
		urlW = 20
	} else if width > 120 {
		nameW = 20
		urlW = 40
	}

	// Header.
	b.WriteString(fmt.Sprintf("  %-*s %-12s %-*s %s\n",
		nameW, "Provider", "Status", urlW, "Base URL", "Models"))
	b.WriteString(fmt.Sprintf("  %-*s %-12s %-*s %s\n",
		nameW, strings.Repeat("─", nameW-1),
		strings.Repeat("─", 11),
		urlW, strings.Repeat("─", urlW-1),
		strings.Repeat("─", 10)))

	for i, p := range providers {
		cursorChar := "  "
		if i == cursor {
			cursorChar = styles.Cursor.Render("> ")
		}

		status := styles.AvailableStatus.Render("[available]")
		if !p.Available {
			status = styles.OfflineStatus.Render("[offline]  ")
		}

		url := Truncate(p.BaseURL, urlW-1)
		modelCount := fmt.Sprintf("%d models", len(p.Models))
		if len(p.Models) == 0 {
			modelCount = "any model"
		} else if len(p.Models) == 1 {
			modelCount = "1 model"
		}

		name := p.DisplayName
		if name == "" {
			name = p.ID
		}

		providerStyle := lipgloss.NewStyle().Foreground(styles.ProviderColor(p.ID))
		coloredName := providerStyle.Render(Truncate(name, 15))

		b.WriteString(fmt.Sprintf("%s%-*s %s %-*s %s\n",
			cursorChar, nameW, coloredName, status, urlW, url, modelCount))
	}

	b.WriteString("\n" + styles.Subtle.Render("j/k: navigate  Esc: back"))

	return b.String()
}
