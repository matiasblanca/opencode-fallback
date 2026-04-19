package transform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTransformSystem_IdentityPrepended(t *testing.T) {
	original := "You have access to tools.\n\n# Code References\nfile.ts"
	blocks := TransformSystem(original, "")

	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks, got %d", len(blocks))
	}

	// First block (without billing) should be identity.
	if blocks[0].Text != claudeCodeIdentity {
		t.Errorf("first block = %q, want %q", blocks[0].Text, claudeCodeIdentity)
	}

	// Second block should be the sanitized original.
	if blocks[1].Text != original {
		t.Errorf("second block = %q, want %q", blocks[1].Text, original)
	}
}

func TestTransformSystem_BillingHeaderFirst(t *testing.T) {
	billing := "cc_version=2.1.87.6ff; cc_entrypoint=sdk-cli; cch=4ffc3;"
	blocks := TransformSystem("Hello world.", billing)

	if len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks, got %d", len(blocks))
	}

	// First block: billing header.
	expected := "x-anthropic-billing-header: " + billing
	if blocks[0].Text != expected {
		t.Errorf("billing block = %q, want %q", blocks[0].Text, expected)
	}

	// Second block: identity.
	if blocks[1].Text != claudeCodeIdentity {
		t.Errorf("identity block = %q, want %q", blocks[1].Text, claudeCodeIdentity)
	}
}

func TestTransformSystem_RemovesOpenCodeIdentity(t *testing.T) {
	original := "You are OpenCode, the best coding agent on the planet.\n\nYou have access to tools.\n\n# Code References\nfile.ts"
	blocks := TransformSystem(original, "")

	// Should have 2 blocks: identity + remaining content.
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	// Verify OpenCode paragraph was removed.
	for _, b := range blocks {
		if strings.Contains(b.Text, "You are OpenCode") {
			t.Error("OpenCode identity should be removed")
		}
	}
}

func TestTransformSystem_RemovesOpenCodeURLs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		removed string
	}{
		{
			name:    "removes github url paragraph",
			input:   "First para.\n\nCheck github.com/anomalyco/opencode for more.\n\nLast para.",
			removed: "github.com/anomalyco/opencode",
		},
		{
			name:    "removes docs url paragraph",
			input:   "First para.\n\nSee opencode.ai/docs for help.\n\nLast para.",
			removed: "opencode.ai/docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := TransformSystem(tt.input, "")
			for _, b := range blocks {
				if strings.Contains(b.Text, tt.removed) {
					t.Errorf("block should not contain %q: %q", tt.removed, b.Text)
				}
			}
		})
	}
}

func TestTransformSystem_ReplacesOpenCodeInline(t *testing.T) {
	original := "If you're unsure, check if OpenCode honestly knows."
	blocks := TransformSystem(original, "")

	found := false
	for _, b := range blocks {
		if strings.Contains(b.Text, "if the assistant honestly") {
			found = true
		}
		if strings.Contains(b.Text, "if OpenCode honestly") {
			t.Error("should replace 'if OpenCode honestly' with 'if the assistant honestly'")
		}
	}
	if !found {
		t.Error("expected to find replacement text")
	}
}

func TestPrefixToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bash", "mcp_Bash"},
		{"read_file", "mcp_Read_file"},
		{"write", "mcp_Write"},
		{"glob", "mcp_Glob"},
		{"StructuredOutput", "StructuredOutput"}, // exception
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := PrefixToolName(tt.input)
			if got != tt.want {
				t.Errorf("PrefixToolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnprefixToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"mcp_Bash", "bash"},
		{"mcp_Read_file", "read_file"},
		{"mcp_Write", "write"},
		{"mcp_Glob", "glob"},
		{"StructuredOutput", "StructuredOutput"}, // exception
		{"no_prefix", "no_prefix"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := UnprefixToolName(tt.input)
			if got != tt.want {
				t.Errorf("UnprefixToolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPrefixToolName_Roundtrip(t *testing.T) {
	names := []string{"bash", "read_file", "write", "glob", "edit"}
	for _, name := range names {
		prefixed := PrefixToolName(name)
		unprefixed := UnprefixToolName(prefixed)
		if unprefixed != name {
			t.Errorf("roundtrip(%q) = prefix(%q) → unprefix(%q) = %q, want %q",
				name, prefixed, prefixed, unprefixed, name)
		}
	}
}

func TestStripToolPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strip mcp_ prefix from tool name",
			input: `{"type":"tool_use","name": "mcp_Bash","id":"123"}`,
			want:  `{"type":"tool_use","name": "bash","id":"123"}`,
		},
		{
			name:  "strip mcp_ prefix with no space",
			input: `{"name":"mcp_Read_file"}`,
			want:  `{"name": "read_file"}`,
		},
		{
			name:  "no prefix to strip",
			input: `{"name": "bash"}`,
			want:  `{"name": "bash"}`,
		},
		{
			name:  "preserves non-tool data",
			input: `{"type":"text","text":"hello"}`,
			want:  `{"type":"text","text":"hello"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripToolPrefix(tt.input)
			if got != tt.want {
				t.Errorf("StripToolPrefix() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

func TestRewriteRequestBody_EndToEnd(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"system": "You are OpenCode, the best coding agent.\n\nYou have access to tools.",
		"messages": [
			{"role": "user", "content": "hello world test message"},
			{"role": "assistant", "content": "I'll help you."}
		],
		"tools": [
			{"name": "bash", "description": "Run bash commands"},
			{"name": "read_file", "description": "Read a file"},
			{"name": "StructuredOutput", "description": "Output structured data"}
		]
	}`

	result, err := RewriteRequestBody(input)
	if err != nil {
		t.Fatalf("RewriteRequestBody failed: %v", err)
	}

	// Parse the result.
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &body); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Verify system is now an array.
	var systemBlocks []SystemBlock
	if err := json.Unmarshal(body["system"], &systemBlocks); err != nil {
		t.Fatalf("unmarshal system blocks: %v", err)
	}

	// First block should be billing header.
	if !strings.HasPrefix(systemBlocks[0].Text, "x-anthropic-billing-header:") {
		t.Errorf("first system block should be billing header, got %q", systemBlocks[0].Text)
	}

	// Verify billing header contains correct cch.
	if !strings.Contains(systemBlocks[0].Text, "cch=4ffc3") {
		t.Errorf("billing header should contain cch=4ffc3, got %q", systemBlocks[0].Text)
	}

	// Second block should be identity.
	if systemBlocks[1].Text != claudeCodeIdentity {
		t.Errorf("second system block = %q, want %q", systemBlocks[1].Text, claudeCodeIdentity)
	}

	// Third block should NOT contain OpenCode identity.
	if len(systemBlocks) > 2 {
		if strings.Contains(systemBlocks[2].Text, "You are OpenCode") {
			t.Error("OpenCode identity should be removed from system")
		}
		if !strings.Contains(systemBlocks[2].Text, "You have access to tools.") {
			t.Error("generic system content should be preserved")
		}
	}

	// Verify tool names are prefixed.
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body["tools"], &tools); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}

	toolNames := map[string]bool{}
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["mcp_Bash"] {
		t.Error("expected tool name mcp_Bash")
	}
	if !toolNames["mcp_Read_file"] {
		t.Error("expected tool name mcp_Read_file")
	}
	if !toolNames["StructuredOutput"] {
		t.Error("expected StructuredOutput to NOT be prefixed")
	}
	if toolNames["bash"] {
		t.Error("original tool name 'bash' should be replaced")
	}
}

func TestRewriteRequestBody_NoMessages(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4",
		"max_tokens": 4096,
		"messages": []
	}`

	result, err := RewriteRequestBody(input)
	if err != nil {
		t.Fatalf("RewriteRequestBody failed: %v", err)
	}

	var body map[string]json.RawMessage
	json.Unmarshal([]byte(result), &body)

	var systemBlocks []SystemBlock
	json.Unmarshal(body["system"], &systemBlocks)

	// With no user messages, billing header should not be present.
	for _, block := range systemBlocks {
		if strings.HasPrefix(block.Text, "x-anthropic-billing-header:") {
			t.Error("billing header should not be present when no user messages")
		}
	}
}

func TestBuildBillingBlock(t *testing.T) {
	tests := []struct {
		name     string
		messages string
		wantCCH  string
	}{
		{
			name:     "with user message",
			messages: `[{"role":"user","content":"hello world test message"}]`,
			wantCCH:  "cch=4ffc3",
		},
		{
			name:     "no user messages",
			messages: `[{"role":"assistant","content":"hi"}]`,
			wantCCH:  "",
		},
		{
			name:     "empty messages",
			messages: `[]`,
			wantCCH:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildBillingBlock(json.RawMessage(tt.messages))
			if tt.wantCCH == "" {
				if got != "" {
					t.Errorf("expected empty billing, got %q", got)
				}
			} else {
				if !strings.Contains(got, tt.wantCCH) {
					t.Errorf("billing = %q, should contain %q", got, tt.wantCCH)
				}
			}
		})
	}
}
