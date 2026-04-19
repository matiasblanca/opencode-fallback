package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/auth"
	"github.com/matiasblanca/opencode-fallback/internal/circuit"
	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/fallback"
	"github.com/matiasblanca/opencode-fallback/internal/logging"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/proxy"
)

// runServe starts the proxy in standalone mode on the configured port.
// The proxy listens for OpenAI-compatible requests and dispatches them
// through the fallback chain.
func runServe(args []string) error {
	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create logger.
	logger := logging.New(cfg.Proxy.LogLevel, nil)

	// Build provider registry.
	registry := buildRegistry(cfg, logger)
	if registry.Len() == 0 {
		return fmt.Errorf("no providers configured or detected — set at least one API key")
	}

	// Build circuit breakers.
	breakers := buildBreakers(cfg, registry, logger)

	// Build chain selector.
	selector := buildSelector(cfg, registry, breakers, logger)

	// Create handler and server.
	handler := proxy.NewHandler(selector, logger)
	server := proxy.NewServer(cfg.Proxy.Host, cfg.Proxy.Port, handler, logger)

	// Handle graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	logger.Info("starting opencode-fallback proxy",
		"host", cfg.Proxy.Host,
		"port", cfg.Proxy.Port,
		"providers", registry.List(),
	)

	return server.Start()
}

// buildRegistry creates a provider registry from the config, registering
// all configured providers. Provider type is determined by pcfg.Type; if
// empty, it is inferred from the provider name ("anthropic" → "anthropic",
// everything else → "openai-compatible").
//
// Supports subscription-based providers ("anthropic-oauth", "github-copilot")
// that read credentials from OpenCode's auth.json via auth.Reader.
func buildRegistry(cfg config.Config, logger *slog.Logger) *provider.Registry {
	reg := provider.NewRegistry()

	// Create a shared auth reader for subscription-based providers.
	// Only created once, shared across all providers that need it.
	var authReader *auth.Reader

	for name, pcfg := range cfg.Providers {
		providerType := pcfg.Type
		if providerType == "" {
			if name == "anthropic" {
				providerType = "anthropic"
			} else {
				providerType = "openai-compatible"
			}
		}

		switch providerType {
		case "anthropic":
			p := provider.NewAnthropicProvider(pcfg.BaseURL, pcfg.APIKey, pcfg.Models, logger)
			if p.IsAvailable() {
				reg.Register(p)
			}

		case "anthropic-oauth":
			if authReader == nil {
				authReader = auth.NewReader(logger)
			}
			p := provider.NewAnthropicOAuthProvider(authReader, logger)
			if p.IsAvailable() {
				reg.Register(p)
				logger.Info("registered subscription provider",
					"provider", p.ID(),
					"name", p.Name(),
				)
			} else {
				logger.Debug("anthropic-oauth not available (no OAuth entry in auth.json)")
			}

		case "github-copilot":
			if authReader == nil {
				authReader = auth.NewReader(logger)
			}
			p := provider.NewCopilotProvider(authReader, logger)
			if p.IsAvailable() {
				reg.Register(p)
				logger.Info("registered subscription provider",
					"provider", p.ID(),
					"name", p.Name(),
				)
			} else {
				logger.Debug("github-copilot not available (no OAuth entry in auth.json)")
			}

		default: // "openai-compatible", "gemini", or any future type
			authType := provider.AuthTypeBearer
			if pcfg.AuthType == "none" {
				authType = provider.AuthTypeNone
			}
			displayName := pcfg.DisplayName
			if displayName == "" {
				displayName = name
			}
			p := provider.NewGenericOpenAIProvider(name, displayName, pcfg.BaseURL, pcfg.APIKey, authType, pcfg.Models, nil, logger)
			if p.IsAvailable() {
				reg.Register(p)
			}
		}
	}

	return reg
}

// buildBreakers creates a circuit breaker for each registered provider.
func buildBreakers(cfg config.Config, registry *provider.Registry, logger *slog.Logger) map[string]*circuit.CircuitBreaker {
	cbCfg := circuit.Config{
		FailureThreshold: cfg.CircuitBreaker.FailureThreshold,
		FailureWindow:    time.Duration(cfg.CircuitBreaker.FailureWindowSeconds) * time.Second,
		OpenDuration:     time.Duration(cfg.CircuitBreaker.OpenDurationSeconds) * time.Second,
	}

	breakers := make(map[string]*circuit.CircuitBreaker)
	for _, id := range registry.List() {
		breakers[id] = circuit.NewWithConfig(id, cbCfg, logger)
	}
	return breakers
}

// buildSelector creates a chain selector from the config.
func buildSelector(
	cfg config.Config,
	registry *provider.Registry,
	breakers map[string]*circuit.CircuitBreaker,
	logger *slog.Logger,
) *fallback.ChainSelector {
	// Convert config chain entries to fallback.ChainConfig.
	global := convertChainEntries(cfg.FallbackChains.Global)

	var groups map[string][]fallback.ChainConfig
	if cfg.FallbackChains.Groups != nil {
		groups = make(map[string][]fallback.ChainConfig)
		for pattern, entries := range cfg.FallbackChains.Groups {
			groups[pattern] = convertChainEntries(entries)
		}
	}

	var agents map[string][]fallback.ChainConfig
	if cfg.FallbackChains.Agents != nil {
		agents = make(map[string][]fallback.ChainConfig)
		for name, entries := range cfg.FallbackChains.Agents {
			agents[name] = convertChainEntries(entries)
		}
	}

	return fallback.NewChainSelector(global, groups, agents, registry, breakers, logger)
}

// convertChainEntries converts config.ChainEntry to fallback.ChainConfig.
func convertChainEntries(entries []config.ChainEntry) []fallback.ChainConfig {
	result := make([]fallback.ChainConfig, len(entries))
	for i, e := range entries {
		result[i] = fallback.ChainConfig{
			ProviderID: e.Provider,
			ModelID:    e.Model,
		}
	}
	return result
}
