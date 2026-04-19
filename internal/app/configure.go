package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
	"github.com/matiasblanca/opencode-fallback/internal/bridge"
	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/opencode"
	"github.com/matiasblanca/opencode-fallback/internal/tui"
)

// runConfigure opens the TUI configurator for fallback chains.
func runConfigure(args []string) error {
	// Parse --opencode-config flag.
	opencodePath := defaultOpenCodePath()
	for i, arg := range args {
		if arg == "--opencode-config" && i+1 < len(args) {
			opencodePath = args[i+1]
		}
	}

	// Create a silent logger for TUI-side status checks (no output to terminal).
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create auth reader and bridge client for status checks.
	// These are optional — if auth.json or bridge token doesn't exist,
	// the status simply shows "not configured" / "disconnected".
	authReader := auth.NewReader(logger)

	// Load config once to read bridge port.
	initialCfg, _ := config.Load()
	bridgePort := initialCfg.Bridge.Port
	if bridgePort <= 0 {
		bridgePort = 18787
	}
	bridgeClient := bridge.NewClientWithConfig(bridgePort, logger)

	deps := tui.Dependencies{
		LoadConfig: config.Load,
		SaveConfig: config.Save,
		ListProviders: func(cfg config.Config) []tui.ProviderInfo {
			return listProvidersForTUI(cfg)
		},
		ResolveChain: func(cfg config.Config, agentName string) []config.ChainEntry {
			return resolveChainForTUI(cfg, agentName)
		},
		DiscoverAgents: func(path string) []opencode.AgentInfo {
			agents, _ := opencode.ParseAgents(path)
			return agents
		},
		GetStatus: func() tui.StatusInfo {
			return getStatusForTUI(authReader, bridgeClient, bridgePort)
		},
	}

	return tui.Run(deps, opencodePath)
}

// defaultOpenCodePath returns the default path for opencode.json.
func defaultOpenCodePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "opencode", "opencode.json")
}

// listProvidersForTUI converts config providers to TUI provider info.
func listProvidersForTUI(cfg config.Config) []tui.ProviderInfo {
	var providers []tui.ProviderInfo
	for id, pcfg := range cfg.Providers {
		available := pcfg.APIKey != "" || pcfg.AuthType == "none" || pcfg.OAuthToken != ""
		name := pcfg.DisplayName
		if name == "" {
			name = id
		}
		providers = append(providers, tui.ProviderInfo{
			ID:          id,
			DisplayName: name,
			BaseURL:     pcfg.BaseURL,
			Available:   available,
			Models:      pcfg.Models,
		})
	}
	return providers
}

// getStatusForTUI reads bridge and auth status for the TUI.
func getStatusForTUI(authReader *auth.Reader, bridgeClient *bridge.Client, port int) tui.StatusInfo {
	// 1. Check bridge availability.
	bridgeAvailable := false
	if bridgeClient != nil {
		bridgeClient.InvalidateHealthCache()
		bridgeAvailable = bridgeClient.IsAvailable()
	}
	bridgeStatus := tui.BridgeStatus{
		Available: bridgeAvailable,
		Port:      port,
	}

	// 2. Check each subscription provider's auth status.
	var providers []tui.ProviderAuthStatus

	// Check anthropic.
	if authReader != nil {
		entry, _ := authReader.Get("anthropic")
		if entry != nil && entry.Type == "oauth" && entry.OAuth != nil {
			providers = append(providers, tui.ProviderAuthStatus{
				ProviderID: "anthropic",
				AuthType:   "oauth",
				Valid:       !auth.IsExpired(entry.OAuth),
				ExpiresIn:  formatExpiry(entry.OAuth.Expires),
			})
		} else {
			// Provider not configured — show as such.
			providers = append(providers, tui.ProviderAuthStatus{
				ProviderID: "anthropic",
				AuthType:   "",
				Valid:       false,
				ExpiresIn:  "",
			})
		}

		// Check github-copilot.
		entry, _ = authReader.Get("github-copilot")
		if entry != nil && entry.Type == "oauth" && entry.OAuth != nil {
			providers = append(providers, tui.ProviderAuthStatus{
				ProviderID: "github-copilot",
				AuthType:   "oauth",
				Valid:       !auth.IsExpired(entry.OAuth),
				ExpiresIn:  formatExpiry(entry.OAuth.Expires),
			})
		} else {
			providers = append(providers, tui.ProviderAuthStatus{
				ProviderID: "github-copilot",
				AuthType:   "",
				Valid:       false,
				ExpiresIn:  "",
			})
		}
	}

	return tui.StatusInfo{
		Bridge:    bridgeStatus,
		Providers: providers,
	}
}

// formatExpiry converts a millisecond epoch timestamp to a human-readable
// compact duration string.
func formatExpiry(expiresMs int64) string {
	if expiresMs == 0 {
		return "never"
	}

	now := time.Now().UnixMilli()
	if expiresMs <= now {
		return "expired"
	}

	remaining := time.Duration(expiresMs-now) * time.Millisecond

	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60

	if hours > 0 && minutes > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return "<1m"
}

// resolveChainForTUI resolves the effective chain for an agent using the
// 3-level cascade: agent → group → global.
func resolveChainForTUI(cfg config.Config, agentName string) []config.ChainEntry {
	// Level 1: Agent-specific.
	if cfg.FallbackChains.Agents != nil {
		if chain, ok := cfg.FallbackChains.Agents[agentName]; ok {
			return chain
		}
	}

	// Level 2: Group match (glob pattern).
	if cfg.FallbackChains.Groups != nil {
		for pattern, chain := range cfg.FallbackChains.Groups {
			matched, _ := filepath.Match(pattern, agentName)
			if matched {
				return chain
			}
		}
	}

	// Level 3: Global default.
	return cfg.FallbackChains.Global
}
