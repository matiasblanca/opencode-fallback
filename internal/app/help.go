package app

import "fmt"

// printHelp prints the CLI usage text to stdout.
func printHelp() {
	fmt.Print(`opencode-fallback — automatic LLM provider resilience for OpenCode

Usage:
  opencode-fallback <command> [options]

Commands:
  serve       Start the proxy in standalone mode (default port: 8787)
  run         Start the proxy and launch a subprocess (e.g., opencode)
  setup       Configure opencode.json to use the proxy
  version     Print version and exit
  help        Show this help message

Examples:
  opencode-fallback serve
  opencode-fallback run -- opencode
  opencode-fallback setup
  opencode-fallback setup --undo

Documentation:
  https://github.com/matiasblanca/opencode-fallback
`)
}
