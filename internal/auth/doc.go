// Package auth reads OpenCode's authentication tokens from disk.
//
// OpenCode stores API keys and OAuth tokens for all LLM providers in a
// single JSON file at <XDG_DATA_HOME>/opencode/auth.json. This package
// reads that file and provides typed access to entries.
//
// Dependency rules: auth/ is a leaf package — it MUST NOT import
// provider/, fallback/, proxy/, or transform/.
package auth
