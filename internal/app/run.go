package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/logging"
	"github.com/matiasblanca/opencode-fallback/internal/proxy"
)

// runRun starts the proxy and launches a subprocess (typically OpenCode).
// When the subprocess exits, the proxy shuts down automatically.
// Usage: opencode-fallback run -- opencode [args...]
func runRun(args []string) error {
	// Find the "--" separator.
	cmdStart := -1
	for i, arg := range args {
		if arg == "--" {
			cmdStart = i + 1
			break
		}
	}

	if cmdStart < 0 || cmdStart >= len(args) {
		return fmt.Errorf("usage: opencode-fallback run -- <command> [args...]\n  example: opencode-fallback run -- opencode")
	}

	subcmd := args[cmdStart:]

	// Load config and build everything like serve.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.Proxy.LogLevel, nil)

	registry := buildRegistry(cfg, logger)
	if registry.Len() == 0 {
		return fmt.Errorf("no providers configured or detected")
	}

	breakers := buildBreakers(cfg, registry, logger)
	selector := buildSelector(cfg, registry, breakers, logger)
	handler := proxy.NewHandler(selector, breakers, registry, logger)
	server := proxy.NewServer(cfg.Proxy.Host, cfg.Proxy.Port, handler, logger)

	// Start proxy in background.
	proxyErr := make(chan error, 1)
	go func() {
		proxyErr <- server.Start()
	}()

	// Give proxy a moment to start.
	time.Sleep(100 * time.Millisecond)

	// Launch subprocess.
	cmd := exec.Command(subcmd[0], subcmd[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Info("launching subprocess",
		"command", subcmd,
	)

	if err := cmd.Start(); err != nil {
		server.Shutdown(context.Background())
		return fmt.Errorf("start subprocess: %w", err)
	}

	// Handle signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Wait for subprocess to exit or signal.
	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- cmd.Wait()
	}()

	select {
	case err := <-cmdDone:
		logger.Info("subprocess exited")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
		return err

	case <-sigCh:
		logger.Info("signal received, shutting down")
		// Kill subprocess.
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
		return nil
	}
}
