package adapter

import (
	"encoding/json"
	"testing"
)

// --------------------------------------------------------------------------
// ConvertOpenAIToAnthropic — request translation
// --------------------------------------------------------------------------

func TestConvertOpenAIToAnthropicBasic(t *testing.T) {
	openaiReq := OpenAIRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   intPtr(1024),
		Temperature: floatPtr(0.7),
		Stream:      true,
	}

	got := ConvertOpenAIToAnthropic(openaiReq)

	if got.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-sonnet-4-20250514")
	}
	if got.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", got.MaxTokens)
	}
	if got.Temperature == nil || *got.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", got.Temperature)
	}
	if !got.Stream {
		t.Error("Stream = false, want true")
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want %q", got.Messages[0].Role, "user")
	}
	if got.Messages[0].Content != "Hello" {
		t.Errorf("Messages[0].Content = %q, want %q", got.Messages[0].Content, "Hello")
	}
}

func TestConvertOpenAIToAnthropicExtractsSystemMessage(t *testing.T) {
	openaiReq := OpenAIRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: intPtr(1024),
	}

	got := ConvertOpenAIToAnthropic(openaiReq)

	if got.System != "You are a helpful assistant" {
		t.Errorf("System = %q, want %q", got.System, "You are a helpful assistant")
	}
	// System message should be removed from messages list.
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1 (system removed)", len(got.Messages))
	}
	if got.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want user", got.Messages[0].Role)
	}
}

func TestConvertOpenAIToAnthropicDefaultMaxTokens(t *testing.T) {
	openaiReq := OpenAIRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "Hello"},
		},
		// MaxTokens not set — should default to 4096.
	}

	got := ConvertOpenAIToAnthropic(openaiReq)

	if got.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096 (default)", got.MaxTokens)
	}
}

// --------------------------------------------------------------------------
// ConvertAnthropicToOpenAI — response translation
// --------------------------------------------------------------------------

func TestConvertAnthropicToOpenAIBasic(t *testing.T) {
	anthropicResp := AnthropicResponse{
		ID:    "msg_123",
		Type:  "message",
		Role:  "assistant",
		Model: "claude-sonnet-4-20250514",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "Hello! How can I help?"},
		},
		StopReason: "end_turn",
		Usage: AnthropicUsage{
			InputTokens:  10,
			OutputTokens: 20,
		},
	}

	got := ConvertAnthropicToOpenAI(anthropicResp)

	if got.ID != "msg_123" {
		t.Errorf("ID = %q, want %q", got.ID, "msg_123")
	}
	if got.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", got.Object, "chat.completion")
	}
	if got.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-sonnet-4-20250514")
	}
	if len(got.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(got.Choices))
	}
	if got.Choices[0].Message.Content != "Hello! How can I help?" {
		t.Errorf("Content = %q, want %q", got.Choices[0].Message.Content, "Hello! How can I help?")
	}
	if got.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", got.Choices[0].FinishReason, "stop")
	}
	if got.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", got.Usage.CompletionTokens)
	}
}

func TestConvertStopReasonMapping(t *testing.T) {
	tests := []struct {
		anthropicReason string
		openaiReason    string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"", "stop"},
		{"unknown", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.anthropicReason, func(t *testing.T) {
			resp := AnthropicResponse{
				Content:    []AnthropicContentBlock{{Type: "text", Text: "x"}},
				StopReason: tt.anthropicReason,
			}
			got := ConvertAnthropicToOpenAI(resp)
			if got.Choices[0].FinishReason != tt.openaiReason {
				t.Errorf("FinishReason = %q, want %q", got.Choices[0].FinishReason, tt.openaiReason)
			}
		})
	}
}

// --------------------------------------------------------------------------
// JSON round-trip
// --------------------------------------------------------------------------

func TestAnthropicRequestMarshal(t *testing.T) {
	req := AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var got AnthropicRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got.Model != req.Model {
		t.Errorf("Model = %q, want %q", got.Model, req.Model)
	}
}

func TestOpenAIResponseMarshal(t *testing.T) {
	resp := OpenAIResponse{
		ID:     "chatcmpl-123",
		Object: "chat.completion",
		Model:  "claude-sonnet-4-20250514",
		Choices: []OpenAIChoice{
			{
				Index:        0,
				Message:      OpenAIMessage{Role: "assistant", Content: "Hello!"},
				FinishReason: "stop",
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var got OpenAIResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got.Choices[0].Message.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", got.Choices[0].Message.Content, "Hello!")
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func intPtr(v int) *int       { return &v }
func floatPtr(v float64) *float64 { return &v }
