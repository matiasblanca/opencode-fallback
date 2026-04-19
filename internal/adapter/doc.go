// Package adapter implements protocol translation between different LLM
// API formats.
//
// Currently: OpenAI ↔ Anthropic Messages API translation.
//
// The adapter translates:
//   - OpenAI request format → Anthropic Messages API format
//   - Anthropic response format → OpenAI response format
//   - Anthropic SSE stream events → OpenAI SSE stream events
//
// This package has no internal dependencies — it only works with data
// structures and stdlib.
package adapter
