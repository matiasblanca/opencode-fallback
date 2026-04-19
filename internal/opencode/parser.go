package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// AgentInfo represents an agent discovered from opencode.json.
type AgentInfo struct {
	Name        string // Agent key as-is from opencode.json (e.g. "sdd-apply")
	Model       string // Current model (e.g. "anthropic/claude-sonnet-4-6")
	Mode        string // "primary" or "subagent"
	Description string // Agent description
}

// agentEntry is used to unmarshal individual agent entries from opencode.json.
type agentEntry struct {
	Model       string `json:"model"`
	Mode        string `json:"mode"`
	Description string `json:"description"`
}

// ParseAgents reads an opencode.json file and extracts all agents.
// Returns nil, nil if the file does not exist (not an error).
// Returns nil, err if the file exists but cannot be parsed.
func ParseAgents(path string) ([]AgentInfo, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read opencode.json: %w", err)
	}

	var raw struct {
		Agent map[string]agentEntry `json:"agent"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse opencode.json: %w", err)
	}

	if len(raw.Agent) == 0 {
		return nil, nil
	}

	agents := make([]AgentInfo, 0, len(raw.Agent))
	for name, entry := range raw.Agent {
		agents = append(agents, AgentInfo{
			Name:        name,
			Model:       entry.Model,
			Mode:        entry.Mode,
			Description: entry.Description,
		})
	}

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	return agents, nil
}
