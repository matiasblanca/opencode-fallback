package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
// all configured providers.
func buildRegistry(cfg config.Config, logger *slog.Logger) *provider.Registry {
	reg := provider.NewRegistry()

	for name, pcfg := range cfg.Providers {
		switch name {
		case "anthropic":
			p := provider.NewAnthropicProvider(pcfg.BaseURL, pcfg.APIKey, pcfg.Models, logger)
			if p.IsAvailable() {
				reg.Register(p)
			}
		case "openai":
			p := provider.NewOpenAIProvider(pcfg.BaseURL, pcfg.APIKey, pcfg.Models, logger)
			if p.IsAvailable() {
				reg.Register(p)
			}
		case "deepseek":
			p := provider.NewDeepSeekProvider(pcfg.BaseURL, pcfg.APIKey, pcfg.Models, logger)
			if p.IsAvailable() {
				reg.Register(p)
			}
		case "ollama":
			p := provider.NewOllamaProvider(pcfg.BaseURL, pcfg.Models, logger)
			reg.Register(p) // Ollama is always "available" (no API key)
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
