package fallback

import (
	"log/slog"
	"path/filepath"

	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
)

// ChainConfig describes one step in a fallback chain configuration.
type ChainConfig struct {
	ProviderID string
	ModelID    string
}

// ChainSelector resolves which fallback chain to use for a given model/agent.
// It implements the 3-level cascade: agent → group → global.
type ChainSelector struct {
	global   []ChainConfig
	groups   map[string][]ChainConfig
	agents   map[string][]ChainConfig
	registry *provider.Registry
	breakers map[string]*circuit.CircuitBreaker
	logger   *slog.Logger
}

// NewChainSelector creates a chain selector with the given configuration.
func NewChainSelector(
	global []ChainConfig,
	groups map[string][]ChainConfig,
	agents map[string][]ChainConfig,
	registry *provider.Registry,
	breakers map[string]*circuit.CircuitBreaker,
	logger *slog.Logger,
) *ChainSelector {
	return &ChainSelector{
		global:   global,
		groups:   groups,
		agents:   agents,
		registry: registry,
		breakers: breakers,
		logger:   logger,
	}
}

// SelectChain returns the fallback chain for the given model/agent name.
//
// Cascade order:
//  1. Agent-specific chain (exact match)
//  2. Group chain (glob pattern match, e.g. "sdd-*")
//  3. Global default chain
func (s *ChainSelector) SelectChain(modelName string) *Chain {
	// Level 1: Agent-specific.
	if s.agents != nil {
		if entries, ok := s.agents[modelName]; ok {
			s.logger.Debug("using agent-specific chain",
				"agent", modelName,
				"providers", len(entries),
			)
			return s.buildChain(entries)
		}
	}

	// Level 2: Group match.
	if s.groups != nil {
		for pattern, entries := range s.groups {
			if MatchGroup(modelName, pattern) {
				s.logger.Debug("using group chain",
					"agent", modelName,
					"group", pattern,
					"providers", len(entries),
				)
				return s.buildChain(entries)
			}
		}
	}

	// Level 3: Global default.
	s.logger.Debug("using global chain",
		"agent", modelName,
		"providers", len(s.global),
	)
	return s.buildChain(s.global)
}

// buildChain creates a Chain from a list of ChainConfig entries,
// resolving providers from the registry.
func (s *ChainSelector) buildChain(configs []ChainConfig) *Chain {
	var providers []ProviderWithModel
	for _, cfg := range configs {
		p, err := s.registry.Get(cfg.ProviderID)
		if err != nil {
			s.logger.Warn("skipping unknown provider in chain",
				"provider", cfg.ProviderID,
				"error", err,
			)
			continue
		}
		providers = append(providers, ProviderWithModel{
			Provider: p,
			ModelID:  cfg.ModelID,
		})
	}
	return NewChain(providers, s.breakers, s.logger)
}

// MatchGroup checks if a model/agent name matches a glob pattern.
// Uses filepath.Match which supports "*" and "?" wildcards.
func MatchGroup(name, pattern string) bool {
	matched, _ := filepath.Match(pattern, name)
	return matched
}
