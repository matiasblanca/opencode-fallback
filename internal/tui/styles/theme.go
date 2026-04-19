package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Colors for the TUI theme.
var (
	ColorPrimary   = lipgloss.Color("#7D56F4")
	ColorSecondary = lipgloss.Color("#874BFD")
	ColorMuted     = lipgloss.Color("#626262")
	ColorSuccess   = lipgloss.Color("#04B575")
	ColorWarning   = lipgloss.Color("#FF9900")
	ColorDanger    = lipgloss.Color("#FF4444")
	ColorAvailable = lipgloss.Color("#04B575")
	ColorOffline   = lipgloss.Color("#FF4444")
	ColorCustom    = lipgloss.Color("#FF9900")
	ColorGlobal    = lipgloss.Color("#626262")
	ColorText      = lipgloss.Color("#FAFAFA")
	ColorDim       = lipgloss.Color("#888888")
)

// ProviderColor returns a color associated with a provider ID.
func ProviderColor(providerID string) color.Color {
	switch providerID {
	case "anthropic":
		return lipgloss.Color("#D97706") // amber/orange
	case "openai":
		return lipgloss.Color("#10B981") // green
	case "deepseek":
		return lipgloss.Color("#3B82F6") // blue
	case "mistral":
		return lipgloss.Color("#F97316") // orange
	case "gemini":
		return lipgloss.Color("#8B5CF6") // purple
	case "openrouter":
		return lipgloss.Color("#EC4899") // pink
	case "ollama":
		return lipgloss.Color("#6B7280") // gray
	default:
		return lipgloss.Color("#9CA3AF") // default gray
	}
}

// Base styles used across the TUI.
var (
	// Title is the style for the main title bar.
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorText).
		Background(ColorPrimary).
		Padding(0, 1)

	// TabActive is the style for the currently selected tab.
	TabActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorText).
		Background(ColorPrimary).
		Padding(0, 2)

	// TabInactive is the style for unselected tabs.
	TabInactive = lipgloss.NewStyle().
		Foreground(ColorDim).
		Padding(0, 2)

	// Cursor is the style for the selection cursor.
	Cursor = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	// StatusBar is the style for the footer status bar.
	StatusBar = lipgloss.NewStyle().
		Foreground(ColorDim)

	// UnsavedBadge is the style for the [unsaved] indicator.
	UnsavedBadge = lipgloss.NewStyle().
		Foreground(ColorWarning).
		Bold(true)

	// CustomBadge is the style for the [custom] badge.
	CustomBadge = lipgloss.NewStyle().
		Foreground(ColorCustom).
		Bold(true)

	// GlobalBadge is the style for the (uses global) badge.
	GlobalBadge = lipgloss.NewStyle().
		Foreground(ColorGlobal)

	// AvailableStatus is the style for [available] text.
	AvailableStatus = lipgloss.NewStyle().
		Foreground(ColorAvailable)

	// OfflineStatus is the style for [offline] text.
	OfflineStatus = lipgloss.NewStyle().
		Foreground(ColorOffline)

	// SlotBox is the style for the chain editor box.
	SlotBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSecondary).
		Padding(0, 1)

	// Heading is the style for section headings.
	Heading = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorText)

	// Subtle is the style for help text and descriptions.
	Subtle = lipgloss.NewStyle().
		Foreground(ColorDim)

	// Success is the style for success messages.
	Success = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	// Error is the style for error messages.
	Error = lipgloss.NewStyle().
		Foreground(ColorDanger)
)
