// Package circuit implements the circuit breaker pattern per provider.
//
// Dependency rules:
//   - circuit/ MUST NOT import any internal package except logging
//
// States: Closed → Open → HalfOpen → Closed
//
// Default parameters (from the research document):
//   - FailureThreshold: 3 failures
//   - FailureWindow: 1 minute
//   - OpenDuration: 30 seconds
//
// Thread-safe with sync.Mutex. ~100 lines of core logic.
package circuit
