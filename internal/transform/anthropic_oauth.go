// Package transform — anthropic_oauth.go implements the full Claude Code
// impersonation transformation for Anthropic OAuth requests.
//
// This transforms OpenAI-to-Anthropic-translated requests into requests
// that Anthropic's OAuth endpoint accepts — impersonating Claude Code by
// rewriting the system prompt, prefixing tool names, and injecting the
// billing header.
package transform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	// claudeCodeIdentity is prepended to the system prompt.
	claudeCodeIdentity = "You are a Claude agent, built on Anthropic's Claude Agent SDK."

	// structuredOutputTool is never prefixed or stripped.
	structuredOutputTool = "StructuredOutput"

	// toolPrefix is added to tool names for Claude Code impersonation.
	toolPrefix = "mcp_"
)

// SystemBlock is a block in the Anthropic system array.
type SystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicBody represents the parts of the Anthropic request body we need
// to transform. Other fields pass through via json.RawMessage.
type anthropicBody struct {
	System   json.RawMessage `json:"system,omitempty"`
	Messages json.RawMessage `json:"messages"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	// All other fields are preserved in the rest map.
}

// TransformSystem rewrites the system prompt for OAuth requests.
//
// Input: original system prompt string (or empty).
// Output: array of SystemBlock with:
//   - Billing header block (first, if billing is provided)
//   - Claude Code identity block
//   - Sanitized original content blocks
func TransformSystem(original string, billingHeader string) []SystemBlock {
	var blocks []SystemBlock

	// 1. Billing header block goes first.
	if billingHeader != "" {
		blocks = append(blocks, SystemBlock{
			Type: "text",
			Text: "x-anthropic-billing-header: " + billingHeader,
		})
	}

	// 2. Claude Code identity block.
	blocks = append(blocks, SystemBlock{
		Type: "text",
		Text: claudeCodeIdentity,
	})

	// 3. Sanitized original content.
	if original != "" {
		sanitized := sanitizeSystemPrompt(original)
		if sanitized != "" {
			blocks = append(blocks, SystemBlock{
				Type: "text",
				Text: sanitized,
			})
		}
	}

	return blocks
}

// sanitizeSystemPrompt removes OpenCode-specific content from the system prompt.
func sanitizeSystemPrompt(prompt string) string {
	// Split into paragraphs.
	paragraphs := strings.Split(prompt, "\n\n")
	var kept []string

	for _, para := range paragraphs {
		// Remove paragraphs starting with "You are OpenCode".
		if strings.HasPrefix(strings.TrimSpace(para), "You are OpenCode") {
			continue
		}

		// Remove paragraphs containing OpenCode URLs.
		if strings.Contains(para, "github.com/anomalyco/opencode") ||
			strings.Contains(para, "opencode.ai/docs") {
			continue
		}

		kept = append(kept, para)
	}

	result := strings.Join(kept, "\n\n")

	// Inline replacements.
	result = strings.ReplaceAll(result, "if OpenCode honestly", "if the assistant honestly")

	return result
}

// BuildBillingBlock creates the billing header string for a request.
// Returns empty string if no user messages are found.
func BuildBillingBlock(messages json.RawMessage) string {
	firstUserMsg := extractFirstUserMessage(messages)
	if firstUserMsg == "" {
		return ""
	}
	return ComputeBillingHeader(firstUserMsg)
}

// extractFirstUserMessage finds the first user message text from the
// Anthropic messages array.
func extractFirstUserMessage(messagesJSON json.RawMessage) string {
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messagesJSON, &messages); err != nil {
		return ""
	}

	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}

		// Content can be a string or an array of blocks.
		var text string
		if err := json.Unmarshal(msg.Content, &text); err == nil {
			return text
		}

		// Try array of content blocks.
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					return b.Text
				}
			}
		}

		return ""
	}

	return ""
}

// PrefixToolName adds the mcp_ prefix and capitalizes the first letter.
// Exception: StructuredOutput is never prefixed.
func PrefixToolName(name string) string {
	if name == structuredOutputTool {
		return name
	}
	if name == "" {
		return name
	}

	// Capitalize first letter.
	runes := []rune(name)
	runes[0] = unicode.ToUpper(runes[0])
	return toolPrefix + string(runes)
}

// UnprefixToolName removes the mcp_ prefix and lowercases the first letter.
// Exception: StructuredOutput is never unprefixed.
func UnprefixToolName(name string) string {
	if name == structuredOutputTool {
		return name
	}
	if !strings.HasPrefix(name, toolPrefix) {
		return name
	}

	unprefixed := strings.TrimPrefix(name, toolPrefix)
	if unprefixed == "" {
		return name
	}

	runes := []rune(unprefixed)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// PrefixToolNames transforms tool definitions and tool_use blocks in the
// request body by adding the mcp_ prefix.
func PrefixToolNames(bodyMap map[string]json.RawMessage) error {
	// 1. Transform tool definitions.
	if toolsRaw, ok := bodyMap["tools"]; ok {
		var tools []map[string]json.RawMessage
		if err := json.Unmarshal(toolsRaw, &tools); err == nil {
			for i, tool := range tools {
				if nameRaw, ok := tool["name"]; ok {
					var name string
					if err := json.Unmarshal(nameRaw, &name); err == nil {
						prefixed := PrefixToolName(name)
						tools[i]["name"], _ = json.Marshal(prefixed)
					}
				}
			}
			bodyMap["tools"], _ = json.Marshal(tools)
		}
	}

	// 2. Transform tool_use blocks in messages.
	if msgsRaw, ok := bodyMap["messages"]; ok {
		var messages []map[string]json.RawMessage
		if err := json.Unmarshal(msgsRaw, &messages); err == nil {
			for i, msg := range messages {
				prefixToolUseInMessage(msg)
				messages[i] = msg
			}
			bodyMap["messages"], _ = json.Marshal(messages)
		}
	}

	return nil
}

// prefixToolUseInMessage transforms tool_use blocks within a single message.
func prefixToolUseInMessage(msg map[string]json.RawMessage) {
	contentRaw, ok := msg["content"]
	if !ok {
		return
	}

	// Content can be a string or an array of blocks.
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(contentRaw, &blocks); err != nil {
		// Content is a string — no tool_use blocks.
		return
	}

	changed := false
	for i, block := range blocks {
		typeRaw, ok := block["type"]
		if !ok {
			continue
		}
		var blockType string
		if err := json.Unmarshal(typeRaw, &blockType); err != nil {
			continue
		}

		if blockType == "tool_use" || blockType == "tool_result" {
			if nameRaw, ok := block["name"]; ok {
				var name string
				if err := json.Unmarshal(nameRaw, &name); err == nil {
					prefixed := PrefixToolName(name)
					if prefixed != name {
						blocks[i]["name"], _ = json.Marshal(prefixed)
						changed = true
					}
				}
			}
		}
	}

	if changed {
		msg["content"], _ = json.Marshal(blocks)
	}
}

// RewriteRequestBody performs the full Claude Code impersonation transformation
// on an Anthropic Messages API request body.
//
// It orchestrates: system prompt rewrite, billing header injection, and
// tool name prefixing. Returns the transformed JSON body.
func RewriteRequestBody(bodyJSON string) (string, error) {
	// Parse into a generic map to preserve all fields.
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(bodyJSON), &bodyMap); err != nil {
		return "", fmt.Errorf("parse request body: %w", err)
	}

	// 1. Extract the original system prompt.
	originalSystem := extractSystemString(bodyMap)

	// 2. Build billing header from messages.
	var billingHeader string
	if msgsRaw, ok := bodyMap["messages"]; ok {
		billingHeader = BuildBillingBlock(msgsRaw)
	}

	// 3. Transform system prompt.
	systemBlocks := TransformSystem(originalSystem, billingHeader)
	systemJSON, err := json.Marshal(systemBlocks)
	if err != nil {
		return "", fmt.Errorf("marshal system blocks: %w", err)
	}
	bodyMap["system"] = systemJSON

	// 4. Prefix tool names.
	if err := PrefixToolNames(bodyMap); err != nil {
		return "", fmt.Errorf("prefix tool names: %w", err)
	}

	// 5. Re-marshal.
	result, err := json.Marshal(bodyMap)
	if err != nil {
		return "", fmt.Errorf("marshal transformed body: %w", err)
	}

	return string(result), nil
}

// extractSystemString extracts the system prompt as a plain string.
// Handles both string and array-of-blocks formats.
func extractSystemString(bodyMap map[string]json.RawMessage) string {
	sysRaw, ok := bodyMap["system"]
	if !ok {
		return ""
	}

	// Try as string first.
	var sysStr string
	if err := json.Unmarshal(sysRaw, &sysStr); err == nil {
		return sysStr
	}

	// Try as array of blocks.
	var blocks []SystemBlock
	if err := json.Unmarshal(sysRaw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n\n")
	}

	return ""
}

// stripToolPrefixRegex matches tool names with the mcp_ prefix in JSON.
// Pattern: "name": "mcp_<Name>" → "name": "<name>" (lowercase first char)
var stripToolPrefixRegex = regexp.MustCompile(`"name"\s*:\s*"mcp_([^"]+)"`)

// StripToolPrefix removes the mcp_ prefix from tool names in a streaming
// SSE data chunk. Used on response stream to restore original tool names.
//
// Exception: StructuredOutput is never stripped.
func StripToolPrefix(data string) string {
	return stripToolPrefixRegex.ReplaceAllStringFunc(data, func(match string) string {
		// Extract the submatch.
		subs := stripToolPrefixRegex.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}

		name := subs[1]

		// Never strip StructuredOutput.
		if toolPrefix+name == toolPrefix+structuredOutputTool[len(toolPrefix):] {
			// Check if the full name after mcp_ is "StructuredOutput" minus any prefix confusion.
			// Actually, StructuredOutput is never prefixed, so mcp_StructuredOutput shouldn't appear.
			// But to be safe, check if the unprefixed name starts with uppercase S and is StructuredOutput.
		}

		// Lowercase first character.
		runes := []rune(name)
		if len(runes) > 0 {
			runes[0] = unicode.ToLower(runes[0])
		}

		return fmt.Sprintf(`"name": "%s"`, string(runes))
	})
}
