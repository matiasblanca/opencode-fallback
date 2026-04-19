package provider

import (
	"encoding/json"
	"net/http"
)

// Message represents a single message in the conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Tool represents a tool definition sent in the request.
type Tool struct {
	Type     string          `json:"type"`
	Function json.RawMessage `json:"function"`
}

// ProxyRequest is the parsed request that travels through the system.
// It arrives in OpenAI-compatible format from the client.
type ProxyRequest struct {
	// Model is the model identifier requested (e.g. "claude-sonnet-4").
	Model string
	// Messages is the conversation history.
	Messages []Message
	// Stream indicates whether the client requested streaming.
	Stream bool
	// Temperature is optional (nil means use provider default).
	Temperature *float64
	// MaxTokens is optional (nil means use provider default).
	MaxTokens *int
	// Tools contains tool/function definitions, if any.
	Tools []Tool
	// RawBody is the original unparsed request body.
	RawBody json.RawMessage
	// Headers are the original client request headers.
	Headers http.Header
}

// ProxyResponse is a non-streaming response to send back to the client.
type ProxyResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
