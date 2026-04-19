// Package stream implements SSE (Server-Sent Events) parsing, checkpoint
// buffering, and stream recovery detection.
//
// Dependency rules:
//   - stream/ does NOT import provider/ or fallback/
//
// Components:
//   - SSEParser: line-by-line SSE parser that emits SSEEvent structs
//   - CheckpointBuffer: accumulates content during streaming for recovery
//   - StreamEndDetector: classifies how a stream ended (normal, abnormal, timeout)
//
// The stream recovery logic (arming continuation requests) lives here too,
// but the actual re-dispatch to a fallback provider is handled by fallback/.
package stream
