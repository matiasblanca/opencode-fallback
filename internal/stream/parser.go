package stream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SSEEvent represents a parsed Server-Sent Events event.
type SSEEvent struct {
	// Type is the event type field (e.g. "message_start"), empty for OpenAI
	// format which uses only data lines.
	Type string
	// Data is the raw data field content.
	Data string
	// FinishReason is extracted from the JSON data for quick access.
	// Values: "stop", "end_turn", "tool_use", or "" if not finished.
	FinishReason string
	// ContentDelta is the incremental text extracted from the JSON data.
	ContentDelta string
	// IsKeepAlive is true for SSE comment lines (e.g. ": ping").
	IsKeepAlive bool
	// Raw is the original line for replay purposes.
	Raw string
}

// SSEParser reads a streaming HTTP response and emits SSEEvent structs one
// by one. It handles both OpenAI format (data-only lines) and Anthropic
// format (event + data lines).
//
// This parser does NOT import provider/ or fallback/ — it operates purely
// on the SSE wire format.
type SSEParser struct {
	scanner *bufio.Scanner
	reader  io.ReadCloser
}

// NewSSEParser creates a parser that reads SSE events from the given reader.
// The reader is typically an http.Response.Body.
func NewSSEParser(reader io.ReadCloser) *SSEParser {
	return &SSEParser{
		scanner: bufio.NewScanner(reader),
		reader:  reader,
	}
}

// Next returns the next SSE event. It returns io.EOF when the stream ends.
func (p *SSEParser) Next() (SSEEvent, error) {
	for p.scanner.Scan() {
		line := p.scanner.Text()

		// Comment lines (keep-alive pings): lines starting with ":"
		if strings.HasPrefix(line, ":") {
			return SSEEvent{
				IsKeepAlive: true,
				Raw:         line,
			}, nil
		}

		// Empty lines are SSE event delimiters — skip them.
		if line == "" {
			continue
		}

		// Event type line (Anthropic format): "event: <type>"
		if strings.HasPrefix(line, "event: ") {
			eventType := strings.TrimPrefix(line, "event: ")
			if p.scanner.Scan() {
				dataLine := p.scanner.Text()
				return p.parseDataLine(dataLine, eventType), nil
			}
		}

		// Data line (OpenAI format): "data: <json>"
		if strings.HasPrefix(line, "data: ") {
			return p.parseDataLine(line, ""), nil
		}
	}

	if err := p.scanner.Err(); err != nil {
		return SSEEvent{}, fmt.Errorf("scanner error: %w", err)
	}

	return SSEEvent{}, io.EOF
}

// parseDataLine parses a "data: ..." line, extracting finish_reason and
// content delta from the JSON payload when possible.
func (p *SSEParser) parseDataLine(line string, eventType string) SSEEvent {
	data := strings.TrimPrefix(line, "data: ")

	event := SSEEvent{
		Type: eventType,
		Data: data,
		Raw:  line,
	}

	// [DONE] is the OpenAI signal for stream end.
	if data == "[DONE]" {
		event.FinishReason = "stop"
		return event
	}

	// Try to extract structured fields from JSON.
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err == nil {
		if len(chunk.Choices) > 0 {
			event.ContentDelta = chunk.Choices[0].Delta.Content
			if chunk.Choices[0].FinishReason != nil {
				event.FinishReason = *chunk.Choices[0].FinishReason
			}
		}
	}

	return event
}

// Close closes the underlying reader.
func (p *SSEParser) Close() error {
	return p.reader.Close()
}
