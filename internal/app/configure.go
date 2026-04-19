package app

import (
	"os"
	"path/filepath"

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
