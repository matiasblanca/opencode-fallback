package screens

import (
	"fmt"
	"strings"

	"github.com/matiasblanca/opencode-fallback/internal/tui/styles"
)

// RenderStatus renders the full system status screen.
func RenderStatus(status StatusInfo, width int) string {
	var b strings.Builder

	// Header.
	b.WriteString(styles.Subtle.Render("< Back (Esc)"))
	b.WriteString(strings.Repeat(" ", 10))
	b.WriteString(styles.Heading.Render("System Status") + "\n")
	b.WriteString(strings.Repeat("─", min(width, 80)) + "\n\n")

	// Bridge section.
	b.WriteString("  " + styles.Heading.Render("Bridge Plugin") + "\n")
	b.WriteString("  " + strings.Repeat("─", 13) + "\n")

	if status.Bridge.Available {
		b.WriteString(fmt.Sprintf("  Status:   %s    Port: %d\n",
			styles.AvailableStatus.Render("[connected]"),
			status.Bridge.Port))
		b.WriteString("  Mode:     Plugin transforms (up-to-date)\n")
	} else {
		b.WriteString(fmt.Sprintf("  Status:   %s Port: %d\n",
			styles.OfflineStatus.Render("[disconnected]"),
			status.Bridge.Port))
		b.WriteString("  Mode:     Local transforms (Go fallback)\n")
	}

	b.WriteString("\n")

	// Subscription Auth section.
	b.WriteString("  " + styles.Heading.Render("Subscription Auth") + "\n")
	b.WriteString("  " + strings.Repeat("─", 17) + "\n")

	if len(status.Providers) == 0 {
		b.WriteString("  " + styles.Subtle.Render("No subscription providers configured.") + "\n")
	} else {
		// Responsive column widths.
		nameW := 18
		authW := 11
		statusW := 16
		expiresW := 12
		if width < 80 {
			nameW = 14
			authW = 8
			statusW = 14
			expiresW = 10
		}

		// Table header.
		b.WriteString(fmt.Sprintf("  %-*s %-*s %-*s %s\n",
			nameW, "Provider",
			authW, "Auth",
			statusW, "Status",
			"Expires"))
		b.WriteString(fmt.Sprintf("  %-*s %-*s %-*s %s\n",
			nameW, strings.Repeat("─", nameW-2),
			authW, strings.Repeat("─", authW-2),
			statusW, strings.Repeat("─", statusW-2),
			strings.Repeat("─", expiresW)))

		for _, p := range status.Providers {
			name := Truncate(p.ProviderID, nameW-2)

			var authStr string
			if p.AuthType == "" || p.AuthType == "none" {
				authStr = "—"
			} else {
				authStr = p.AuthType
			}

			var statusStr string
			var expiresStr string
			switch {
			case p.AuthType == "" || p.AuthType == "none":
				statusStr = styles.Subtle.Render("[not configured]")
				expiresStr = ""
			case p.Valid:
				statusStr = styles.AvailableStatus.Render("[valid]")
				expiresStr = p.ExpiresIn
			default:
				statusStr = styles.OfflineStatus.Render("[expired]")
				expiresStr = p.ExpiresIn
			}

			b.WriteString(fmt.Sprintf("  %-*s %-*s %-*s %s\n",
				nameW, name,
				authW, authStr,
				statusW, statusStr,
				expiresStr))
		}
	}

	// Footer.
	b.WriteString("\n  " + strings.Repeat("─", min(width-4, 76)) + "\n")
	b.WriteString("  " + styles.Subtle.Render("r: refresh   Esc: back"))

	return b.String()
}

// RenderStatusBar renders a compact single-line status bar for the main screen.
// Returns empty string if terminal height < 15 (to avoid crowding).
func RenderStatusBar(status StatusInfo, width int, height int) string {
	if height > 0 && height < 15 {
		return ""
	}

	var parts []string

	// Bridge status.
	if status.Bridge.Available {
		parts = append(parts, "Bridge: "+styles.AvailableStatus.Render("●")+" connected")
	} else {
		parts = append(parts, "Bridge: "+styles.OfflineStatus.Render("○")+" offline")
	}

	// Provider auth status.
	if len(status.Providers) > 0 {
		var authParts []string
		for _, p := range status.Providers {
			name := compactProviderName(p.ProviderID)
			switch {
			case p.AuthType == "" || p.AuthType == "none":
				// Skip not-configured providers in compact bar.
			case p.Valid:
				expiry := compactExpiry(p.ExpiresIn)
				if expiry != "" {
					authParts = append(authParts, styles.AvailableStatus.Render("●")+" "+name+" ("+expiry+")")
				} else {
					authParts = append(authParts, styles.AvailableStatus.Render("●")+" "+name)
				}
			default:
				authParts = append(authParts, styles.OfflineStatus.Render("✗")+" "+name+" (expired)")
			}
		}
		if len(authParts) > 0 {
			parts = append(parts, "Auth: "+strings.Join(authParts, " "))
		}
	}

	parts = append(parts, styles.Subtle.Render("s: details"))

	line := " " + strings.Join(parts, "   ")

	return styles.Subtle.Render(strings.Repeat("─", min(width, 80))) + "\n" + line
}

// compactProviderName returns a short name for the status bar.
func compactProviderName(providerID string) string {
	switch providerID {
	case "github-copilot":
		return "copilot"
	default:
		return providerID
	}
}

// compactExpiry returns a compact expiry string for the status bar.
// Strips trailing detail: "2h 15m" → "2h", "45m" → "45m", "never" → "never".
func compactExpiry(expiresIn string) string {
	if expiresIn == "" || expiresIn == "never" {
		return expiresIn
	}
	if expiresIn == "expired" {
		return "" // handled by the ✗ indicator
	}
	// Take only the first unit for compactness: "2h 15m" → "2h".
	parts := strings.Fields(expiresIn)
	if len(parts) > 0 {
		return parts[0]
	}
	return expiresIn
}
