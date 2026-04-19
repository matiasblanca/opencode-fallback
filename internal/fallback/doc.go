// Package fallback implements the fallback chain orchestration.
//
// Dependency rules:
//   - fallback/ imports provider/, circuit/, config/
//   - fallback/ does NOT import proxy/ or app/
//
// FallbackChain iterates an ordered list of providers until one responds
// successfully. It accumulates failure records for observability.
// Pattern adapted from Manifest proxy-fallback.service.ts: { success, failures[] }.
//
// ChainSelector resolves which chain to use via 3-level cascade:
//
//	agent → group → global
package fallback
