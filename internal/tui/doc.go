// Package tui implements the Bubbletea-based terminal user interface for
// opencode-fallback configuration and monitoring.
//
// Dependency rules:
//   - tui/ does NOT import proxy/ directly
//   - tui/ receives functions via dependency injection from app/
//
// The TUI is planned for v0.2. In v0.1, all interaction is via CLI commands
// and JSON config editing.
package tui
