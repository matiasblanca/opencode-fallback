package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/matiasblanca/opencode-fallback/internal/config"
)

func testAgentDisplays() []AgentDisplay {
	return []AgentDisplay{
		{Name: "bauhaus-executor", CurrentModel: "anthropic/claude-sonnet-4-6", Mode: "subagent", HasOverride: false},
		{Name: "gentleman", CurrentModel: "anthropic/claude-sonnet-4-6", Mode: "primary", HasOverride: false},
		{Name: "sdd-apply", CurrentModel: "anthropic/claude-sonnet-4-6", Mode: "subagent", HasOverride: true,
			Chain: []config.ChainEntry{
				{Provider: "mistral", Model: "codestral-latest"},
				{Provider: "openai", Model: "gpt-4o"},
			}},
		{Name: "sdd-design", CurrentModel: "github-copilot/gemini-3.1-pro", Mode: "subagent", HasOverride: false},
		{Name: "sdd-explore", CurrentModel: "openai/gpt-5.3-codex", Mode: "subagent", HasOverride: true,
			Chain: []config.ChainEntry{
				{Provider: "deepseek", Model: "deepseek-chat"},
			}},
	}
}

func TestRenderAgents_FiveAgents(t *testing.T) {
	agents := testAgentDisplays()
	output := RenderAgents(agents, 0, 100, 0, 0)

	for _, a := range agents {
		if !strings.Contains(output, a.Name) {
			t.Errorf("output should contain agent name %q", a.Name)
		}
	}

	// Check that custom badges appear for agents with overrides.
	if !strings.Contains(output, "custom") {
		t.Error("should contain [custom] badge for overridden agents")
	}
	if !strings.Contains(output, "uses global") {
		t.Error("should contain '(uses global)' for non-overridden agents")
	}
}

func TestRenderAgents_Empty(t *testing.T) {
	output := RenderAgents(nil, 0, 80, 0, 0)
	if !strings.Contains(output, "No agents") {
		t.Error("should show 'No agents' message when empty")
	}
}

func TestRenderAgents_NoCurrentModel(t *testing.T) {
	agents := []AgentDisplay{
		{Name: "custom-agent", CurrentModel: "", Mode: "", HasOverride: true,
			Chain: []config.ChainEntry{{Provider: "openai", Model: "gpt-4o"}}},
	}
	output := RenderAgents(agents, 0, 80, 0, 0)

	if !strings.Contains(output, "custom-agent") {
		t.Error("should contain the agent name")
	}
	// Should show dash for empty model.
	if !strings.Contains(output, "—") {
		t.Error("should show '—' for empty current model")
	}
}

func TestRenderAgents_CursorRendering(t *testing.T) {
	agents := testAgentDisplays()
	output := RenderAgents(agents, 2, 80, 0, 0)

	if !strings.Contains(output, ">") {
		t.Error("should contain cursor marker '>'")
	}
}

func TestRenderAgents_NarrowWidth(t *testing.T) {
	agents := testAgentDisplays()
	output := RenderAgents(agents, 0, 60, 0, 0)

	// Should not crash and should contain agent names.
	if !strings.Contains(output, "bauhaus-exec") {
		// Name may be truncated at 15 chars.
		if !strings.Contains(output, "bauhaus") {
			t.Error("should contain truncated agent name at narrow width")
		}
	}
	// Should use compressed badges.
	if !strings.Contains(output, "[C]") && !strings.Contains(output, "[G]") {
		t.Error("should use compressed badges at narrow width")
	}
}

func TestRenderAgents_WideWidth(t *testing.T) {
	agents := testAgentDisplays()
	output := RenderAgents(agents, 0, 140, 0, 0)

	// Should use full names (22 char width).
	if !strings.Contains(output, "bauhaus-executor") {
		t.Error("should show full agent name at wide width")
	}
	// Should use full badges.
	if !strings.Contains(output, "custom") {
		t.Error("should show [custom] badge at wide width")
	}
}

func TestRenderAgents_ScrollWith20Agents(t *testing.T) {
	// Create 20 agents.
	var agents []AgentDisplay
	for i := 0; i < 20; i++ {
		agents = append(agents, AgentDisplay{
			Name:         fmt.Sprintf("agent-%02d", i),
			CurrentModel: "anthropic/claude-sonnet-4",
			HasOverride:  false,
		})
	}

	// height=18 → visibleRows=10, scrollOffset=0 → show agents 0-9.
	output := RenderAgents(agents, 0, 100, 0, 18)

	if !strings.Contains(output, "agent-00") {
		t.Error("should show first agent")
	}
	if strings.Contains(output, "agent-15") {
		t.Error("should NOT show agent-15 when only 10 visible")
	}
	// Should show scroll down indicator.
	if !strings.Contains(output, "↓") {
		t.Error("should show scroll down indicator")
	}
}

func TestRenderAgents_ScrollCursorBelowViewport(t *testing.T) {
	// Create 20 agents.
	var agents []AgentDisplay
	for i := 0; i < 20; i++ {
		agents = append(agents, AgentDisplay{
			Name: fmt.Sprintf("agent-%02d", i),
		})
	}

	// scrollOffset=5, cursor=7 (within viewport)
	output := RenderAgents(agents, 7, 100, 5, 18)
	if !strings.Contains(output, "agent-07") {
		t.Error("should show agent at cursor position")
	}
	// Should show scroll up indicator.
	if !strings.Contains(output, "↑") {
		t.Error("should show scroll up indicator when scrollOffset > 0")
	}
}

func TestRenderAgents_ScrollCursorAboveViewport(t *testing.T) {
	var agents []AgentDisplay
	for i := 0; i < 20; i++ {
		agents = append(agents, AgentDisplay{
			Name: fmt.Sprintf("agent-%02d", i),
		})
	}

	// scrollOffset=10, show agents 10-19.
	output := RenderAgents(agents, 12, 100, 10, 18)
	if !strings.Contains(output, "agent-12") {
		t.Error("should show agent at cursor")
	}
	if !strings.Contains(output, "↑ 10 more") {
		t.Error("should show '↑ 10 more'")
	}
}

func TestRenderAgents_ScrollIndicators(t *testing.T) {
	var agents []AgentDisplay
	for i := 0; i < 30; i++ {
		agents = append(agents, AgentDisplay{
			Name: fmt.Sprintf("agent-%02d", i),
		})
	}

	// Middle of list: scrollOffset=5, height=18 (10 visible).
	output := RenderAgents(agents, 8, 100, 5, 18)
	if !strings.Contains(output, "↑") {
		t.Error("should show scroll up indicator")
	}
	if !strings.Contains(output, "↓") {
		t.Error("should show scroll down indicator")
	}
}

func TestRenderAgents_ScrollWithZeroAgents(t *testing.T) {
	output := RenderAgents(nil, 0, 100, 0, 18)
	if !strings.Contains(output, "No agents") {
		t.Error("should show empty message")
	}
}

func TestRenderAgents_ShowsHeader(t *testing.T) {
	agents := testAgentDisplays()
	output := RenderAgents(agents, 0, 100, 0, 0)

	if !strings.Contains(output, "Agent") {
		t.Error("should contain 'Agent' header")
	}
	if !strings.Contains(output, "Current model") {
		t.Error("should contain 'Current model' header")
	}
	if !strings.Contains(output, "Fallback chain") {
		t.Error("should contain 'Fallback chain' header")
	}
}
