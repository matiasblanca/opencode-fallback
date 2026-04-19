package adapter

// --------------------------------------------------------------------------
// OpenAI types (subset used for translation)
// --------------------------------------------------------------------------

// OpenAIRequest represents an OpenAI chat completion request.
type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

// OpenAIMessage represents a single message in OpenAI format.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIResponse represents an OpenAI chat completion response.
type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created,omitempty"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage,omitempty"`
}

// OpenAIChoice represents one completion choice.
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// OpenAIUsage represents token usage in OpenAI format.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --------------------------------------------------------------------------
// Anthropic types
// --------------------------------------------------------------------------

// AnthropicRequest represents an Anthropic Messages API request.
type AnthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []AnthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

// AnthropicMessage represents a single message in Anthropic format.
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AnthropicResponse represents an Anthropic Messages API response.
type AnthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []AnthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      AnthropicUsage          `json:"usage"`
}

// AnthropicContentBlock represents a content block in an Anthropic response.
type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// AnthropicUsage represents token usage in Anthropic format.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --------------------------------------------------------------------------
// Conversion: OpenAI → Anthropic
// --------------------------------------------------------------------------

// ConvertOpenAIToAnthropic translates an OpenAI chat completion request into
// an Anthropic Messages API request.
//
// Key differences handled:
//   - System messages are extracted into the top-level "system" field
//   - max_tokens is required by Anthropic (defaults to 4096 if not set)
//   - Message format is structurally the same but system role is handled differently
func ConvertOpenAIToAnthropic(req OpenAIRequest) AnthropicRequest {
	var system string
	var messages []AnthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			system = msg.Content
			continue
		}
		messages = append(messages, AnthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	return AnthropicRequest{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		Messages:    messages,
		System:      system,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}
}

// --------------------------------------------------------------------------
// Conversion: Anthropic → OpenAI
// --------------------------------------------------------------------------

// ConvertAnthropicToOpenAI translates an Anthropic Messages API response into
// an OpenAI chat completion response.
//
// Key differences handled:
//   - Content blocks are concatenated into a single content string
//   - stop_reason is mapped to OpenAI finish_reason values
//   - Usage fields are renamed (input_tokens → prompt_tokens)
func ConvertAnthropicToOpenAI(resp AnthropicResponse) OpenAIResponse {
	// Concatenate all text content blocks.
	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return OpenAIResponse{
		ID:     resp.ID,
		Object: "chat.completion",
		Model:  resp.Model,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: convertStopReason(resp.StopReason),
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// convertStopReason maps Anthropic stop_reason to OpenAI finish_reason.
func convertStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}
