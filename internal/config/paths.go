package config

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the directory where opencode-fallback stores its configuration.
//
// It uses the OPENCODE_FALLBACK_CONFIG_DIR environment variable when set (useful
// for testing), otherwise falls back to os.UserConfigDir()/opencode-fallback.
func ConfigDir() string {
	if override := os.Getenv("OPENCODE_FALLBACK_CONFIG_DIR"); override != "" {
		return override
	}

	base, err := os.UserConfigDir()
	if err != nil {
		// Last resort: use the current working directory
		base = "."
	}
	return filepath.Join(base, "opencode-fallback")
}

// ConfigFile returns the full path to the config.json file.
func ConfigFile() string {
	return filepath.Join(ConfigDir(), "config.json")
}
