package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/matiasblanca/opencode-fallback/internal/opencode"
)

// Run starts the TUI configurator.
func Run(deps Dependencies, opencodePath string) error {
	// Load config.
	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Discover providers.
	var providers []ProviderInfo
	if deps.ListProviders != nil {
		providers = deps.ListProviders(cfg)
	}

	// Discover agents from opencode.json.
	var agents []opencode.AgentInfo
	if deps.DiscoverAgents != nil {
		agents = deps.DiscoverAgents(opencodePath)
	}

	// Build initial model.
	m := NewModel(cfg, providers, agents, deps)

	// Create and run program.
	// AltScreen is set declaratively in Model.View() (Bubbletea v2 pattern).
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}
