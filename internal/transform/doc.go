// Package transform implements request/response transformations for
// Claude Code impersonation via Anthropic OAuth.
//
// This includes:
//   - CCH billing header computation
//   - System prompt rewriting
//   - Tool name prefixing/stripping
//   - Full request body rewriting
//
// Dependency rules: transform/ is a leaf package — it MUST NOT import
// provider/, fallback/, proxy/, or auth/.
package transform
