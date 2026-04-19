package app

import "fmt"

// runVersion prints the version string and exits.
// The version is injected by GoReleaser via ldflags at build time.
func runVersion(version string) error {
	fmt.Printf("opencode-fallback %s\n", version)
	return nil
}
