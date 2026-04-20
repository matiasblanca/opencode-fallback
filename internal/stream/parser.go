package stream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrTTFTTimeout is returned when the TTFT (Time-To-First-Token) timeout
// expires. This means the provider opened a stream but did not produce
// any SSE events within the configured timeout window.
var ErrTTFTTimeout = fmt.Errorf("TTFT timeout: no SSE event received within deadline")

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

// DataTransformFunc is an optional function applied to each SSE data field
// before the event is returned to the caller. Used by OAuth providers to
// strip tool name prefixes from response streams.
type DataTransformFunc func(data string) string

// SSEParser reads a streaming HTTP response and emits SSEEvent structs one
// by one. It handles both OpenAI format (data-only lines) and Anthropic
// format (event + data lines).
//
// This parser does NOT import provider/ or fallback/ — it operates purely
// on the SSE wire format.
type SSEParser struct {
	scanner   *bufio.Scanner
	reader    io.ReadCloser
	transform DataTransformFunc
	prefix    *SSEEvent // if set, returned on first Next() call
}

// NewSSEParser creates a parser that reads SSE events from the given reader.
// The reader is typically an http.Response.Body.
func NewSSEParser(reader io.ReadCloser) *SSEParser {
	return &SSEParser{
		scanner: bufio.NewScanner(reader),
		reader:  reader,
	}
}

// NewSSEParserWithTransform creates a parser with a data transformation
// function that is applied to each SSE data field before the event is
// returned. This is used by OAuth providers to strip tool name prefixes.
func NewSSEParserWithTransform(reader io.ReadCloser, fn DataTransformFunc) *SSEParser {
	return &SSEParser{
		scanner:   bufio.NewScanner(reader),
		reader:    reader,
		transform: fn,
	}
}

// Next returns the next SSE event. It returns io.EOF when the stream ends.
func (p *SSEParser) Next() (SSEEvent, error) {
	// Return buffered prefix event first (used by TTFT check).
	if p.prefix != nil {
		ev := *p.prefix
		p.prefix = nil
		return ev, nil
	}

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

	// Apply optional transform to the data.
	if p.transform != nil {
		data = p.transform(data)
	}

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

// NextWithTimeout returns the next SSE event, but returns an error if no
// event arrives within the given timeout. This is used for TTFT (Time-To-
// First-Token) detection — if a provider opens a stream but hangs without
// producing events, we need to detect it and fail over.
//
// The timeout only applies to the FIRST call. Subsequent calls should use
// a longer timeout or no timeout (the model is actively streaming).
func (p *SSEParser) NextWithTimeout(timeout time.Duration) (SSEEvent, error) {
	type result struct {
		event SSEEvent
		err   error
	}

	ch := make(chan result, 1)
	go func() {
		ev, err := p.Next()
		ch <- result{ev, err}
	}()

	select {
	case r := <-ch:
		return r.event, r.err
	case <-time.After(timeout):
		return SSEEvent{}, ErrTTFTTimeout
	}
}

// NewPrefixedParser creates a parser that returns the given event on the
// first call to Next(), then delegates to the underlying parser for all
// subsequent events. Used after TTFT timeout verification — the first
// event was consumed to prove the stream is alive and must be replayed.
func NewPrefixedParser(parser *SSEParser, first SSEEvent) *SSEParser {
	parser.prefix = &first
	return parser
}
