package screens

import (
	"strings"
	"testing"
)

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   StatusInfo
		width    int
		contains []string
		absent   []string
	}{
		{
			name: "all healthy - bridge connected, providers valid",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: true, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: true, ExpiresIn: "2h 15m"},
					{ProviderID: "github-copilot", AuthType: "oauth", Valid: true, ExpiresIn: "never"},
				},
			},
			width: 100,
			contains: []string{
				"System Status",
				"Bridge Plugin",
				"connected",
				"18787",
				"Plugin transforms",
				"Subscription Auth",
				"anthropic",
				"oauth",
				"valid",
				"2h 15m",
				"github-copilot",
				"never",
				"r: refresh",
			},
		},
		{
			name: "bridge disconnected",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: false, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: true, ExpiresIn: "1h"},
				},
			},
			width: 100,
			contains: []string{
				"disconnected",
				"Local transforms",
				"Go fallback",
			},
			absent: []string{
				"Plugin transforms",
			},
		},
		{
			name: "provider expired",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: true, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: false, ExpiresIn: "expired"},
				},
			},
			width: 100,
			contains: []string{
				"expired",
				"anthropic",
			},
		},
		{
			name: "no providers configured",
			status: StatusInfo{
				Bridge:    BridgeStatus{Available: true, Port: 18787},
				Providers: nil,
			},
			width: 80,
			contains: []string{
				"No subscription providers configured",
			},
			absent: []string{
				"Expires",
				"oauth",
			},
		},
		{
			name: "mixed status - valid, expired, not configured",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: false, Port: 19999},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: true, ExpiresIn: "45m"},
					{ProviderID: "github-copilot", AuthType: "", Valid: false, ExpiresIn: ""},
				},
			},
			width: 100,
			contains: []string{
				"disconnected",
				"19999",
				"anthropic",
				"valid",
				"45m",
				"not configured",
			},
		},
		{
			name: "narrow width",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: true, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: true, ExpiresIn: "2h"},
				},
			},
			width: 60,
			contains: []string{
				"System Status",
				"anthropic",
				"connected",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := RenderStatus(tt.status, tt.width)

			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q, but doesn't.\nOutput:\n%s", want, output)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(output, absent) {
					t.Errorf("output should NOT contain %q, but does.\nOutput:\n%s", absent, output)
				}
			}
		})
	}
}

func TestRenderStatusBar(t *testing.T) {
	tests := []struct {
		name     string
		status   StatusInfo
		width    int
		height   int
		contains []string
		absent   []string
		empty    bool
	}{
		{
			name: "all healthy",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: true, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: true, ExpiresIn: "2h 15m"},
					{ProviderID: "github-copilot", AuthType: "oauth", Valid: true, ExpiresIn: "never"},
				},
			},
			width:  100,
			height: 30,
			contains: []string{
				"Bridge:",
				"●",
				"connected",
				"anthropic",
				"copilot",
				"s: details",
			},
		},
		{
			name: "bridge offline and expired token",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: false, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "anthropic", AuthType: "oauth", Valid: false, ExpiresIn: "expired"},
				},
			},
			width:  100,
			height: 30,
			contains: []string{
				"○",
				"offline",
				"✗",
				"expired",
			},
		},
		{
			name: "too small terminal height",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: true, Port: 18787},
			},
			width:  80,
			height: 12,
			empty:  true,
		},
		{
			name: "no subscription providers",
			status: StatusInfo{
				Bridge:    BridgeStatus{Available: true, Port: 18787},
				Providers: nil,
			},
			width:  80,
			height: 30,
			contains: []string{
				"Bridge:",
				"connected",
				"s: details",
			},
			absent: []string{
				"Auth:",
			},
		},
		{
			name: "not configured providers skipped in bar",
			status: StatusInfo{
				Bridge: BridgeStatus{Available: true, Port: 18787},
				Providers: []ProviderAuthStatus{
					{ProviderID: "github-copilot", AuthType: "", Valid: false, ExpiresIn: ""},
				},
			},
			width:  80,
			height: 30,
			contains: []string{
				"Bridge:",
				"connected",
			},
			absent: []string{
				"Auth:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := RenderStatusBar(tt.status, tt.width, tt.height)

			if tt.empty {
				if output != "" {
					t.Errorf("expected empty output for small terminal, got: %q", output)
				}
				return
			}

			if output == "" {
				t.Error("expected non-empty status bar output")
				return
			}

			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q, but doesn't.\nOutput:\n%s", want, output)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(output, absent) {
					t.Errorf("output should NOT contain %q, but does.\nOutput:\n%s", absent, output)
				}
			}
		})
	}
}

func TestCompactProviderName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github-copilot", "copilot"},
		{"anthropic", "anthropic"},
		{"openai", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := compactProviderName(tt.input)
			if got != tt.want {
				t.Errorf("compactProviderName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompactExpiry(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2h 15m", "2h"},
		{"45m", "45m"},
		{"never", "never"},
		{"expired", ""},
		{"", ""},
		{"5h", "5h"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := compactExpiry(tt.input)
			if got != tt.want {
				t.Errorf("compactExpiry(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
