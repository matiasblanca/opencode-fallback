package screens

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// ProviderInfo mirrors tui.ProviderInfo to avoid circular imports.
type ProviderInfo struct {
	ID          string
	DisplayName string
	BaseURL     string
	Available   bool
	Models      []string
}

// AgentDisplay mirrors tui.AgentDisplay to avoid circular imports.
type AgentDisplay struct {
	Name         string
	CurrentModel string
	Mode         string
	HasOverride  bool
	Chain        []config.ChainEntry
}

// StatusInfo holds status information for the status screen.
// Mirrors tui.StatusInfo to avoid circular imports.
type StatusInfo struct {
	Bridge    BridgeStatus
	Providers []ProviderAuthStatus
}

// BridgeStatus represents the bridge plugin connection state.
type BridgeStatus struct {
	Available bool
	Port      int
}

// ProviderAuthStatus represents the auth status for a subscription provider.
type ProviderAuthStatus struct {
	ProviderID string
	AuthType   string
	Valid      bool
	ExpiresIn  string
}

// RenderHeader renders the top section with title, tabs, and unsaved indicator.
func RenderHeader(activeTab int, dirty bool, statusMsg string, width int) string {
	title := styles.Title.Render(" opencode-fallback configure ")

	// Tab bar (compressed for narrow terminals).
	var tabs string
	tabNames := []string{"1: Global", "2: Agents"}
	for i, name := range tabNames {
		if width < 60 {
			// Compressed tab labels.
			shortNames := []string{"1:Glo", "2:Agt"}
			name = shortNames[i]
		}
		if i == activeTab {
			if width < 60 {
				tabs += styles.TabActive.Render("[" + name + "]")
			} else {
				tabs += styles.TabActive.Render("[" + name + "]")
			}
		} else {
			tabs += styles.TabInactive.Render("[" + name + "]")
		}
		if width >= 60 {
			tabs += " "
		}
	}

	// Right side: unsaved indicator + status.
	var right string
	if dirty {
		right = styles.UnsavedBadge.Render("[unsaved]") + " "
	}
	if statusMsg != "" {
		if strings.HasPrefix(statusMsg, "Error") {
			right += styles.Error.Render(statusMsg)
		} else {
			right += styles.Success.Render(statusMsg)
		}
	}

	header := title + "\n\n" + tabs
	if right != "" {
		// Pad to right align.
		header += "  " + right
	}
	header += "\n" + strings.Repeat("─", min(width, 80))

	return header
}

// RenderFooter renders contextual keybind hints at the bottom.
// scrollInfo is an optional scroll indicator string to prepend.
func RenderFooter(screen int, activeTab int, width int, scrollInfo ...string) string {
	var hints string
	// screen 0 = ScreenMain
	if screen == 0 {
		if activeTab == 0 {
			hints = "j/k: navigate  Enter: edit slot  x: clear slot  s: status  Ctrl+S: save  ?: help  q: quit"
		} else {
			hints = "j/k: navigate  Enter: edit  n: add agent  p: providers  s: status  ?: help  q: quit"
		}
	}

	var result string
	if len(scrollInfo) > 0 && scrollInfo[0] != "" {
		result = scrollInfo[0] + "\n"
	}
	result += styles.Subtle.Render(hints)
	return result
}

// RenderConfirmQuit renders the quit confirmation dialog.
func RenderConfirmQuit(width int) string {
	box := styles.SlotBox.Width(min(width-4, 60)).Render(
		styles.Heading.Render("Unsaved changes") + "\n\n" +
			"Save before quitting?\n\n" +
			styles.Subtle.Render("y: save and quit  n: quit without saving  Esc: cancel"),
	)
	return "\n\n" + box
}

// Screen constants (mirrors tui.Screen to avoid import cycle).
const (
	HelpScreenMain        = 0
	HelpScreenChainEditor = 1
	HelpScreenModelPicker = 2
	HelpScreenProviders   = 3
	HelpScreenStatus      = 4
)

// RenderHelp renders the contextual help overlay.
// screen is the current screen (use HelpScreen* constants), activeTab is 0=Global, 1=Agents.
func RenderHelp(screen int, activeTab int, width int, height int) string {
	var b strings.Builder
	b.WriteString(styles.Heading.Render("Keybindings") + "\n\n")

	switch screen {
	case HelpScreenMain:
		if activeTab == 0 {
			// Global tab.
			b.WriteString("Navigation:\n")
			b.WriteString("  j/k, ↑/↓    Move cursor\n")
			b.WriteString("  Enter       Edit slot\n")
			b.WriteString("  Tab         Switch tab\n")
			b.WriteString("  1 / 2       Go to Global / Agents\n\n")
			b.WriteString("Actions:\n")
			b.WriteString("  x           Clear slot\n")
			b.WriteString("  Ctrl+S      Save configuration\n")
			b.WriteString("  p           View providers\n")
			b.WriteString("  s           System status\n")
			b.WriteString("  ?           Toggle help\n")
			b.WriteString("  q           Quit\n")
		} else {
			// Agents tab.
			b.WriteString("Navigation:\n")
			b.WriteString("  j/k, ↑/↓    Move cursor\n")
			b.WriteString("  Enter       Edit agent chain\n")
			b.WriteString("  Tab         Switch tab\n")
			b.WriteString("  1 / 2       Go to Global / Agents\n\n")
			b.WriteString("Actions:\n")
			b.WriteString("  n           Add agent\n")
			b.WriteString("  Ctrl+S      Save configuration\n")
			b.WriteString("  p           View providers\n")
			b.WriteString("  s           System status\n")
			b.WriteString("  ?           Toggle help\n")
			b.WriteString("  q           Quit\n")
		}

	case HelpScreenChainEditor:
		b.WriteString("Navigation:\n")
		b.WriteString("  j/k, ↑/↓    Move cursor\n")
		b.WriteString("  Enter       Select model for slot\n")
		b.WriteString("  Esc         Go back\n\n")
		b.WriteString("Actions:\n")
		b.WriteString("  x           Clear slot\n")
		b.WriteString("  d           Delete override (use global)\n")
		b.WriteString("  o           Create override\n")
		b.WriteString("  Ctrl+S      Save configuration\n")

	case HelpScreenModelPicker:
		b.WriteString("Navigation:\n")
		b.WriteString("  ↑/↓         Move cursor\n")
		b.WriteString("  Enter       Select model\n")
		b.WriteString("  Esc         Cancel\n\n")
		b.WriteString("Filter:\n")
		b.WriteString("  Type        Filter models\n")
		b.WriteString("  Backspace   Delete filter char\n")

	case HelpScreenProviders:
		b.WriteString("Navigation:\n")
		b.WriteString("  j/k, ↑/↓    Move cursor\n")
		b.WriteString("  Esc         Go back\n")
		b.WriteString("  q           Quit\n")

	case HelpScreenStatus:
		b.WriteString("Actions:\n")
		b.WriteString("  r           Refresh status\n")
		b.WriteString("  Esc         Go back\n")
	}

	b.WriteString("\n" + styles.Subtle.Render("Press any key to close"))

	return styles.SlotBox.Width(min(width-4, 60)).Render(b.String())
}

// RenderAddAgentInput renders the inline input for adding a new agent.
func RenderAddAgentInput(input string, width int) string {
	prompt := "New agent name: " + input + "_"
	return "\n" + styles.Heading.Render(prompt) + "\n"
}

// FormatChainSummary formats a chain as "model1 -> model2 -> ...".
// If maxLen is provided (> 0), the result is truncated.
func FormatChainSummary(chain []config.ChainEntry, maxLen ...int) string {
	if len(chain) == 0 {
		return "(empty)"
	}
	parts := make([]string, len(chain))
	for i, e := range chain {
		parts[i] = e.Provider + "/" + e.Model
	}
	result := strings.Join(parts, " → ")
	if len(maxLen) > 0 && maxLen[0] > 0 {
		result = Truncate(result, maxLen[0])
	}
	return result
}

// FormatChainSummaryColored formats a chain with provider-colored models.
func FormatChainSummaryColored(chain []config.ChainEntry, maxLen int) string {
	if len(chain) == 0 {
		return "(empty)"
	}
	parts := make([]string, len(chain))
	for i, e := range chain {
		modelColor := lipgloss.NewStyle().Foreground(styles.ProviderColor(e.Provider))
		parts[i] = modelColor.Render(e.Provider + "/" + e.Model)
	}
	return strings.Join(parts, " → ")
}

// slotLabel returns the display label for a slot index.
func SlotLabel(index int) string {
	if index == 0 {
		return "Primary   "
	}
	return fmt.Sprintf("Fallback %d", index)
}

// Truncate truncates a string to maxLen runes, adding "..." if truncated.
// Uses rune count instead of byte count for correct unicode handling.
func Truncate(s string, maxLen int) string {
	runeCount := utf8.RuneCountInString(s)
	if runeCount <= maxLen {
		return s
	}
	if maxLen < 4 {
		runes := []rune(s)
		return string(runes[:maxLen])
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}

// RenderScrollIndicator returns a position indicator for scrollable lists.
// Returns "" if no items. Returns "[current/total]" style indicator with directional arrows.
func RenderScrollIndicator(current, total, visible int) string {
	if total == 0 {
		return ""
	}
	var parts []string
	if current > 0 {
		parts = append(parts, fmt.Sprintf("▲ %d more", current))
	}
	parts = append(parts, fmt.Sprintf("[%d/%d]", current+1, total))
	if current+visible < total {
		remaining := total - current - visible
		if remaining < 0 {
			remaining = 0
		}
		parts = append(parts, fmt.Sprintf("▼ %d more", remaining))
	}
	return styles.Subtle.Render(strings.Join(parts, "  "))
}

// ProviderStatusStr returns "[available]", "[offline]", or "[unknown]" for a provider.
func ProviderStatusStr(providerID string, providers []ProviderInfo) string {
	for _, p := range providers {
		if p.ID == providerID {
			if p.Available {
				return styles.AvailableStatus.Render("[available]")
			}
			return styles.OfflineStatus.Render("[offline]")
		}
	}
	return styles.Subtle.Render("[unknown]")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
