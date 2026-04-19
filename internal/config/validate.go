package config

import "fmt"

// Validate checks a Config for correctness and returns a slice of all errors
// found. An empty slice means the config is valid.
//
// Validation rules:
//   - Version must be "1"
//   - Proxy port must be in the range 1–65535
//   - Proxy host must not be empty
//   - At least one provider must be configured
//   - Global fallback chain must not be empty
//   - Each chain entry must reference a configured provider
//   - Each chain entry must specify a non-empty model
//   - Circuit breaker values (threshold, window, open duration) must be positive
func Validate(cfg Config) []error {
	var errs []error

	// Version
	if cfg.Version != "1" {
		errs = append(errs, fmt.Errorf("invalid version %q: must be \"1\"", cfg.Version))
	}

	// Proxy port
	if cfg.Proxy.Port < 1 || cfg.Proxy.Port > 65535 {
		errs = append(errs, fmt.Errorf("invalid proxy port %d: must be 1–65535", cfg.Proxy.Port))
	}

	// Proxy host
	if cfg.Proxy.Host == "" {
		errs = append(errs, fmt.Errorf("proxy host must not be empty"))
	}

	// Providers
	if len(cfg.Providers) == 0 {
		errs = append(errs, fmt.Errorf("at least one provider must be configured"))
	}

	// Global chain
	if len(cfg.FallbackChains.Global) == 0 {
		errs = append(errs, fmt.Errorf("global fallback chain must not be empty"))
	}

	// Chain entries
	for i, entry := range cfg.FallbackChains.Global {
		if _, ok := cfg.Providers[entry.Provider]; !ok {
			errs = append(errs, fmt.Errorf(
				"global chain entry %d references unknown provider %q", i, entry.Provider))
		}
		if entry.Model == "" {
			errs = append(errs, fmt.Errorf(
				"global chain entry %d (provider %q) has empty model", i, entry.Provider))
		}
	}

	// Circuit breaker
	if cfg.CircuitBreaker.FailureThreshold <= 0 {
		errs = append(errs, fmt.Errorf(
			"circuit_breaker.failure_threshold must be positive, got %d",
			cfg.CircuitBreaker.FailureThreshold))
	}
	if cfg.CircuitBreaker.FailureWindowSeconds <= 0 {
		errs = append(errs, fmt.Errorf(
			"circuit_breaker.failure_window_seconds must be positive, got %d",
			cfg.CircuitBreaker.FailureWindowSeconds))
	}
	if cfg.CircuitBreaker.OpenDurationSeconds <= 0 {
		errs = append(errs, fmt.Errorf(
			"circuit_breaker.open_duration_seconds must be positive, got %d",
			cfg.CircuitBreaker.OpenDurationSeconds))
	}

	return errs
}
