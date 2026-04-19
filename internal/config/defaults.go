package config

// DefaultConfig returns a Config populated with sensible default values.
//
// This is used in zero-config mode (no config file found) and as the
// baseline for newly created configurations.
func DefaultConfig() Config {
	return Config{
		Version: "1",
		Proxy: ProxyConfig{
			Port:     8787,
			Host:     "127.0.0.1",
			LogLevel: "info",
		},
		Providers: map[string]ProviderConfig{},
		FallbackChains: FallbackChainsConfig{
			Global: []ChainEntry{},
		},
		CircuitBreaker: CircuitBreakerConfig{
			FailureThreshold:     3,
			FailureWindowSeconds: 60,
			OpenDurationSeconds:  30,
		},
		Timeouts: TimeoutConfig{
			ConnectSeconds:           5,
			FirstTokenSeconds:        60,
			HeartbeatSeconds:         30,
			ReasoningModelMultiplier: 2.0,
		},
		StreamRecovery: StreamRecoveryConfig{
			Enabled:            true,
			ContinuationPrompt: "Continue exactly from where you left off. Do not repeat any content.",
		},
	}
}
