package app

import (
	"fmt"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/logging"
	"github.com/matiasblanca/opencode-fallback/internal/opencode"
)

// runSetup modifies opencode.json to repoint provider baseURLs to the proxy.
// It creates a backup of the original config before modifying.
// Usage: opencode-fallback setup [--undo]
func runSetup(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.Proxy.LogLevel, nil)

	proxyURL := fmt.Sprintf("http://%s:%d/v1", cfg.Proxy.Host, cfg.Proxy.Port)

	// Find opencode.json.
	ocPath := opencode.FindConfigPath()

	ocCfg, err := opencode.Load(ocPath)
	if err != nil {
		return fmt.Errorf("load opencode config from %s: %w", ocPath, err)
	}

	// Create backup.
	backupPath, err := opencode.Backup(ocPath)
	if err != nil {
		return fmt.Errorf("backup opencode config: %w", err)
	}
	logger.Info("backup created", "path", backupPath)

	// Set baseURL for each known provider.
	providers := []string{"anthropic", "openai", "deepseek", "ollama"}
	count := 0
	for _, p := range providers {
		existing := ocCfg.GetProviderBaseURL(p)
		if existing != "" || isConfiguredProvider(cfg, p) {
			if err := ocCfg.SetProviderBaseURL(p, proxyURL); err != nil {
				logger.Warn("failed to set baseURL", "provider", p, "error", err)
				continue
			}
			count++
		}
	}

	if count == 0 {
		return fmt.Errorf("no providers found in opencode config to repoint")
	}

	if err := ocCfg.Save(ocPath); err != nil {
		return fmt.Errorf("save opencode config: %w", err)
	}

	logger.Info("setup complete",
		"providers_repointed", count,
		"proxy_url", proxyURL,
	)

	fmt.Printf("✓ setup complete: %d providers repointed to %s\n", count, proxyURL)
	fmt.Printf("  backup saved to %s\n", backupPath)
	fmt.Printf("  to undo: restore the backup manually\n")

	return nil
}

// isConfiguredProvider checks if a provider exists in the fallback config.
func isConfiguredProvider(cfg config.Config, name string) bool {
	_, ok := cfg.Providers[name]
	return ok
}
