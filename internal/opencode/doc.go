// Package opencode handles reading, writing, and modifying opencode.json
// configuration files.
//
// The setup command uses this package to repoint provider baseURLs to the
// proxy (localhost:8787) and to restore the original configuration on undo.
//
// It supports detecting opencode.json in multiple locations:
//   - Project-local: .opencode/config.json
//   - User-level Linux/macOS: ~/.config/opencode/config.json
//   - User-level Windows: %APPDATA%\opencode\config.json
package opencode
