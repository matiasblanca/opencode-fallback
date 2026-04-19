// Package app is the top-level command dispatcher for opencode-fallback.
//
// It is the only package that imports all other internal packages and
// assembles them together. All commands (serve, run, setup, config, version)
// are dispatched from here.
package app

import "fmt"

// Run dispatches the CLI command based on the provided arguments.
// It is called from main.go with os.Args[1:] and the build version.
func Run(args []string, version string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "run":
		return runRun(args[1:])
	case "setup":
		return runSetup(args[1:])
	case "configure":
		return runConfigure(args[1:])
	case "version", "--version", "-v":
		return runVersion(version)
	case "help", "--help", "-h":
		printHelp()
		return nil
	default:
		return fmt.Errorf("unknown command: %s. Run 'opencode-fallback help' for usage", args[0])
	}
}
