package screens

import (
	"fmt"
	"strings"

	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// RenderAgents renders the agent list with their fallback chains.
// scrollOffset controls which agents are visible, height controls visible rows.
func RenderAgents(agents []AgentDisplay, cursor int, width int, scrollOffset int, height int) string {
	if len(agents) == 0 {
		return styles.Subtle.Render("No agents discovered.\n\nMake sure opencode.json exists at ~/.config/opencode/opencode.json\nor use 'n' to add an agent manually.\n")
	}

	var b strings.Builder

	// Compute responsive column widths.
	var nameW, modelW, chainW int
	if width < 80 {
		nameW = 15
		modelW = 20
		chainW = 0 // Compressed: badge only, no chain detail
	} else if width <= 120 {
		nameW = 18
		modelW = 24
		chainW = width - nameW - modelW - 8
	} else {
		nameW = 22
		modelW = 28
		chainW = width - nameW - modelW - 8
	}

	// Header.
	b.WriteString(fmt.Sprintf("  %-*s %-*s %s\n",
		nameW, "Agent",
		modelW, "Current model",
		"Fallback chain"))
	b.WriteString(fmt.Sprintf("  %-*s %-*s %s\n",
		nameW, strings.Repeat("─", nameW-1),
		modelW, strings.Repeat("─", modelW-1),
		strings.Repeat("─", min(chainW, 30))))

	// Calculate visible range for pagination.
	visibleRows := len(agents) // Default: show all.
	if height > 0 {
		visibleRows = height - 8 // Account for header + footer + tabs + padding.
		if visibleRows < 3 {
			visibleRows = 3
		}
	}

	startIdx := scrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + visibleRows
	if endIdx > len(agents) {
		endIdx = len(agents)
	}

	// Scroll up indicator.
	if startIdx > 0 {
		b.WriteString(styles.Subtle.Render(fmt.Sprintf("  ↑ %d more\n", startIdx)))
	}

	// Render visible agents.
	for i := startIdx; i < endIdx; i++ {
		agent := agents[i]
		cursorChar := "  "
		if i == cursor {
			cursorChar = styles.Cursor.Render("> ")
		}

		name := Truncate(agent.Name, nameW-1)
		model := Truncate(agent.CurrentModel, modelW-1)
		if model == "" {
			model = styles.Subtle.Render("—")
		}

		var chainStr string
		if chainW <= 0 {
			// Compressed mode: badge only.
			if agent.HasOverride {
				chainStr = styles.CustomBadge.Render("[C]")
			} else {
				chainStr = styles.GlobalBadge.Render("[G]")
			}
		} else if agent.HasOverride {
			chainStr = styles.CustomBadge.Render("[custom]") + " " + FormatChainSummaryColored(agent.Chain, chainW-10)
		} else {
			chainStr = styles.GlobalBadge.Render("(uses global)")
		}

		b.WriteString(fmt.Sprintf("%s%-*s %-*s %s\n",
			cursorChar, nameW, name, modelW, model, chainStr))
	}

	// Scroll down indicator.
	if endIdx < len(agents) {
		remaining := len(agents) - endIdx
		b.WriteString(styles.Subtle.Render(fmt.Sprintf("  ↓ %d more\n", remaining)))
	}

	return b.String()
}
