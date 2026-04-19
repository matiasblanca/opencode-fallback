package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

const validOpenCodeJSON = `{
  "$schema": "https://opencode.ai/config.json",
  "agent": {
    "gentleman": {
      "description": "Senior Architect mentor",
      "mode": "primary",
      "model": "anthropic/claude-sonnet-4-6",
      "prompt": "test"
    },
    "sdd-apply": {
      "description": "Implement code changes from task definitions",
      "hidden": true,
      "mode": "subagent",
      "model": "anthropic/claude-sonnet-4-6",
      "prompt": "test"
    },
    "sdd-explore": {
      "description": "Investigate codebase and think through ideas",
      "mode": "subagent",
      "model": "openai/gpt-5.3-codex"
    },
    "sdd-design": {
      "description": "Create technical design from proposals",
      "mode": "subagent",
      "model": "github-copilot/gemini-3.1-pro-preview"
    },
    "bauhaus-executor": {
      "description": "Execute bauhaus-kb processing/analysis prompts",
      "mode": "subagent",
      "model": "anthropic/claude-sonnet-4-6"
    }
  },
  "mcp": {},
  "permission": {}
}`

func TestParseAgents_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(validOpenCodeJSON), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	agents, err := ParseAgents(path)
	if err != nil {
		t.Fatalf("ParseAgents() error = %v", err)
	}

	if len(agents) != 5 {
		t.Fatalf("ParseAgents() returned %d agents, want 5", len(agents))
	}

	// Verify first agent has correct fields.
	// Since sorted alphabetically, first should be "bauhaus-executor".
	first := agents[0]
	if first.Name != "bauhaus-executor" {
		t.Errorf("first agent Name = %q, want %q", first.Name, "bauhaus-executor")
	}
	if first.Model != "anthropic/claude-sonnet-4-6" {
		t.Errorf("first agent Model = %q, want %q", first.Model, "anthropic/claude-sonnet-4-6")
	}
	if first.Mode != "subagent" {
		t.Errorf("first agent Mode = %q, want %q", first.Mode, "subagent")
	}
	if first.Description != "Execute bauhaus-kb processing/analysis prompts" {
		t.Errorf("first agent Description = %q", first.Description)
	}
}

func TestParseAgents_NonExistentFile(t *testing.T) {
	agents, err := ParseAgents("/nonexistent/path/opencode.json")
	if err != nil {
		t.Errorf("ParseAgents() error = %v, want nil for non-existent file", err)
	}
	if agents != nil {
		t.Errorf("ParseAgents() = %v, want nil for non-existent file", agents)
	}
}

func TestParseAgents_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	agents, err := ParseAgents(path)
	if err == nil {
		t.Error("ParseAgents() expected error for malformed JSON")
	}
	if agents != nil {
		t.Error("ParseAgents() expected nil for malformed JSON")
	}
}

func TestParseAgents_NoAgentKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	content := `{"provider": {}, "mcp": {}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	agents, err := ParseAgents(path)
	if err != nil {
		t.Errorf("ParseAgents() error = %v, want nil", err)
	}
	if len(agents) != 0 {
		t.Errorf("ParseAgents() returned %d agents, want 0", len(agents))
	}
}

func TestParseAgents_AlphabeticalOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(validOpenCodeJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	agents, err := ParseAgents(path)
	if err != nil {
		t.Fatalf("ParseAgents() error = %v", err)
	}

	expectedOrder := []string{
		"bauhaus-executor",
		"gentleman",
		"sdd-apply",
		"sdd-design",
		"sdd-explore",
	}

	for i, want := range expectedOrder {
		if agents[i].Name != want {
			t.Errorf("agents[%d].Name = %q, want %q", i, agents[i].Name, want)
		}
	}
}

func TestParseAgents_PrimaryMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(validOpenCodeJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	agents, err := ParseAgents(path)
	if err != nil {
		t.Fatalf("ParseAgents() error = %v", err)
	}

	// "gentleman" is the only primary agent in our fixture.
	var gentleman *AgentInfo
	for i := range agents {
		if agents[i].Name == "gentleman" {
			gentleman = &agents[i]
			break
		}
	}
	if gentleman == nil {
		t.Fatal("gentleman not found in agents")
	}
	if gentleman.Mode != "primary" {
		t.Errorf("gentleman.Mode = %q, want %q", gentleman.Mode, "primary")
	}
}
