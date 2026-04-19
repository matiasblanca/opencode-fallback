// Package main is the entrypoint for opencode-fallback.
//
// opencode-fallback is a local proxy that provides automatic LLM provider
// resilience for coding agents in OpenCode. When a primary provider fails
// (rate limit, timeout, overload), it automatically falls back to the next
// provider in a configured chain — transparently, without user intervention.
//
// Version is injected via ldflags at build time by GoReleaser.
package main

import (
	"fmt"
	"os"

	"github.com/matiasblanca/opencode-fallback/internal/app"
)

// version is set by GoReleaser via ldflags.
// -ldflags "-X main.version=v0.1.0"
var version = "dev"

func main() {
	if err := app.Run(os.Args[1:], version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
