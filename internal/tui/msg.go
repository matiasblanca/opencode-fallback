package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// SaveResultMsg is sent after a save attempt completes.
type SaveResultMsg struct {
	Err error
}

// ClearStatusMsg clears the transient status message.
type ClearStatusMsg struct{}

// clearStatusAfter returns a Cmd that clears the status message after a delay.
func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}
