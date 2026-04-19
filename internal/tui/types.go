package tui

import (
	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/opencode"
)

// Dependencies defines what tui/ needs from the outside world.
// Provided by app/ via constructor injection.
type Dependencies struct {
	// LoadConfig loads the current configuration from disk.
	LoadConfig func() (config.Config, error)

	// SaveConfig writes the configuration to disk atomically.
	SaveConfig func(config.Config) error

	// ListProviders returns info about all configured/detected providers.
	ListProviders func(cfg config.Config) []ProviderInfo

	// ResolveChain resolves the effective chain for an agent,
	// applying the 3-level cascade (agent -> group -> global).
	ResolveChain func(cfg config.Config, agentName string) []config.ChainEntry

	// DiscoverAgents reads the opencode.json and returns all agents found.
	// Returns nil/empty if the file doesn't exist (not an error).
	DiscoverAgents func(opencodePath string) []opencode.AgentInfo

	// GetStatus returns real-time auth and bridge status information.
	// Called on demand (not polled) — the TUI refreshes when the user navigates
	// to the status screen or presses 'r' to refresh.
	GetStatus func() StatusInfo
}

// ProviderInfo holds display information about a provider.
type ProviderInfo struct {
	ID          string
	DisplayName string
	BaseURL     string
	Available   bool
	Models      []string
}

// AgentDisplay is a merged view of an agent for the TUI.
type AgentDisplay struct {
	Name         string             // Agent name
	CurrentModel string             // Model assigned in opencode.json (empty if not in opencode)
	Mode         string             // "primary" / "subagent" / ""
	HasOverride  bool               // true if has custom chain in config.agents
	Chain        []config.ChainEntry // Resolved fallback chain (from cascade)
}

// StatusInfo holds real-time status information for the TUI status bar.
type StatusInfo struct {
	Bridge    BridgeStatus
	Providers []ProviderAuthStatus
}

// BridgeStatus represents the bridge plugin connection state.
type BridgeStatus struct {
	Available bool // Whether the bridge HTTP server is responding
	Port      int  // Configured bridge port
}

// ProviderAuthStatus represents the auth status for a subscription provider.
type ProviderAuthStatus struct {
	ProviderID string // e.g. "anthropic", "github-copilot"
	AuthType   string // "oauth", "api", or "none"
	Valid      bool   // Token is present and not expired
	ExpiresIn  string // Human-readable time until expiry (e.g. "2h 15m", "never", "expired")
}
