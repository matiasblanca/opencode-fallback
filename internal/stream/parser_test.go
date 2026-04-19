package stream

import (
	"io"
	"strings"
	"testing"
)

// nopCloser wraps an io.Reader to satisfy io.ReadCloser.
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

// newTestParser creates an SSEParser from a raw string.
func newTestParser(data string) *SSEParser {
	return NewSSEParser(nopCloser{strings.NewReader(data)})
}

// --------------------------------------------------------------------------
// SSEParser — basic parsing
// --------------------------------------------------------------------------

func TestSSEParserDataLine(t *testing.T) {
	input := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n"
	p := newTestParser(input)

	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.ContentDelta != "hello" {
		t.Errorf("ContentDelta = %q, want %q", ev.ContentDelta, "hello")
	}
	if ev.FinishReason != "" {
		t.Errorf("FinishReason = %q, want empty", ev.FinishReason)
	}
	if ev.IsKeepAlive {
		t.Error("IsKeepAlive = true, want false")
	}
}

func TestSSEParserDoneEvent(t *testing.T) {
	input := "data: [DONE]\n\n"
	p := newTestParser(input)

	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.Data != "[DONE]" {
		t.Errorf("Data = %q, want %q", ev.Data, "[DONE]")
	}
	if ev.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", ev.FinishReason, "stop")
	}
}

func TestSSEParserKeepAlive(t *testing.T) {
	input := ": ping\n\n"
	p := newTestParser(input)

	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ev.IsKeepAlive {
		t.Error("IsKeepAlive = false, want true")
	}
	if ev.Raw != ": ping" {
		t.Errorf("Raw = %q, want %q", ev.Raw, ": ping")
	}
}

func TestSSEParserEventType(t *testing.T) {
	input := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	p := newTestParser(input)

	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.Type != "message_start" {
		t.Errorf("Type = %q, want %q", ev.Type, "message_start")
	}
}

func TestSSEParserEOF(t *testing.T) {
	input := "data: [DONE]\n\n"
	p := newTestParser(input)

	_, _ = p.Next() // consume the event
	_, err := p.Next()
	if err != io.EOF {
		t.Errorf("Next() error = %v, want io.EOF", err)
	}
}

func TestSSEParserEmptyInput(t *testing.T) {
	p := newTestParser("")
	_, err := p.Next()
	if err != io.EOF {
		t.Errorf("Next() error = %v, want io.EOF", err)
	}
}

func TestSSEParserFinishReasonStop(t *testing.T) {
	input := "data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n"
	p := newTestParser(input)

	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", ev.FinishReason, "stop")
	}
}

func TestSSEParserNonJSONData(t *testing.T) {
	input := "data: not json at all\n\n"
	p := newTestParser(input)

	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ev.Data != "not json at all" {
		t.Errorf("Data = %q, want %q", ev.Data, "not json at all")
	}
	// No crash on non-JSON — content fields stay empty.
	if ev.ContentDelta != "" {
		t.Errorf("ContentDelta = %q, want empty", ev.ContentDelta)
	}
}

func TestSSEParserMultipleEvents(t *testing.T) {
	input := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}",
		"",
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}",
		"",
		"data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}",
		"",
		"data: [DONE]",
		"",
	}, "\n")

	p := newTestParser(input)

	// Event 1
	ev, err := p.Next()
	if err != nil {
		t.Fatalf("event 1: error = %v", err)
	}
	if ev.ContentDelta != "Hello" {
		t.Errorf("event 1: ContentDelta = %q, want %q", ev.ContentDelta, "Hello")
	}

	// Event 2
	ev, err = p.Next()
	if err != nil {
		t.Fatalf("event 2: error = %v", err)
	}
	if ev.ContentDelta != " world" {
		t.Errorf("event 2: ContentDelta = %q, want %q", ev.ContentDelta, " world")
	}

	// Event 3
	ev, err = p.Next()
	if err != nil {
		t.Fatalf("event 3: error = %v", err)
	}
	if ev.FinishReason != "stop" {
		t.Errorf("event 3: FinishReason = %q, want %q", ev.FinishReason, "stop")
	}

	// Event 4 — [DONE]
	ev, err = p.Next()
	if err != nil {
		t.Fatalf("event 4: error = %v", err)
	}
	if ev.Data != "[DONE]" {
		t.Errorf("event 4: Data = %q, want %q", ev.Data, "[DONE]")
	}

	// EOF
	_, err = p.Next()
	if err != io.EOF {
		t.Errorf("after all events: error = %v, want io.EOF", err)
	}
}

func TestSSEParserClose(t *testing.T) {
	p := newTestParser("data: test\n\n")
	if err := p.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// --------------------------------------------------------------------------
// CheckpointBuffer
// --------------------------------------------------------------------------

func TestCheckpointBufferEmpty(t *testing.T) {
	buf := NewCheckpointBuffer()
	if !buf.IsEmpty() {
		t.Error("new buffer IsEmpty() = false, want true")
	}
	if buf.Text() != "" {
		t.Errorf("new buffer Text() = %q, want empty", buf.Text())
	}
	if buf.TokenCount() != 0 {
		t.Errorf("new buffer TokenCount() = %d, want 0", buf.TokenCount())
	}
	if buf.EventCount() != 0 {
		t.Errorf("new buffer EventCount() = %d, want 0", buf.EventCount())
	}
}

func TestCheckpointBufferAppend(t *testing.T) {
	buf := NewCheckpointBuffer()

	buf.Append(SSEEvent{ContentDelta: "Hello"})
	buf.Append(SSEEvent{ContentDelta: " world"})

	if buf.IsEmpty() {
		t.Error("IsEmpty() = true after appending, want false")
	}
	if got := buf.Text(); got != "Hello world" {
		t.Errorf("Text() = %q, want %q", got, "Hello world")
	}
	if got := buf.TokenCount(); got != 2 {
		t.Errorf("TokenCount() = %d, want 2", got)
	}
}

func TestCheckpointBufferKeepAliveDoesNotAddContent(t *testing.T) {
	buf := NewCheckpointBuffer()
	buf.Append(SSEEvent{IsKeepAlive: true, Raw: ": ping"})

	if !buf.IsEmpty() {
		t.Error("IsEmpty() = false after keep-alive, want true")
	}
	if buf.TokenCount() != 0 {
		t.Errorf("TokenCount() = %d after keep-alive, want 0", buf.TokenCount())
	}
	// But the event is still recorded.
	if buf.EventCount() != 1 {
		t.Errorf("EventCount() = %d after keep-alive, want 1", buf.EventCount())
	}
}

func TestCheckpointBufferEventCount(t *testing.T) {
	buf := NewCheckpointBuffer()
	buf.Append(SSEEvent{ContentDelta: "a"})
	buf.Append(SSEEvent{ContentDelta: "b"})
	buf.Append(SSEEvent{ContentDelta: "c"})

	if got := buf.EventCount(); got != 3 {
		t.Errorf("EventCount() = %d, want 3", got)
	}
}

func TestCheckpointBufferEvents(t *testing.T) {
	buf := NewCheckpointBuffer()
	ev1 := SSEEvent{ContentDelta: "a"}
	ev2 := SSEEvent{ContentDelta: "b"}
	buf.Append(ev1)
	buf.Append(ev2)

	events := buf.Events()
	if len(events) != 2 {
		t.Fatalf("Events() len = %d, want 2", len(events))
	}
	if events[0].ContentDelta != "a" {
		t.Errorf("Events()[0].ContentDelta = %q, want %q", events[0].ContentDelta, "a")
	}
	if events[1].ContentDelta != "b" {
		t.Errorf("Events()[1].ContentDelta = %q, want %q", events[1].ContentDelta, "b")
	}
}

// --------------------------------------------------------------------------
// StreamEndType detection
// --------------------------------------------------------------------------

func TestDetectStreamEnd(t *testing.T) {
	tests := []struct {
		name      string
		lastEvent SSEEvent
		err       error
		want      StreamEndType
	}{
		{
			name:      "DONE event",
			lastEvent: SSEEvent{Data: "[DONE]"},
			err:       nil,
			want:      StreamEndNormal,
		},
		{
			name:      "finish_reason stop",
			lastEvent: SSEEvent{FinishReason: "stop"},
			err:       nil,
			want:      StreamEndNormal,
		},
		{
			name:      "finish_reason end_turn",
			lastEvent: SSEEvent{FinishReason: "end_turn"},
			err:       nil,
			want:      StreamEndNormal,
		},
		{
			name:      "EOF without finish_reason",
			lastEvent: SSEEvent{},
			err:       io.EOF,
			want:      StreamEndAbnormal,
		},
		{
			name:      "no finish_reason no error",
			lastEvent: SSEEvent{},
			err:       nil,
			want:      StreamEndTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectStreamEnd(tt.lastEvent, tt.err)
			if got != tt.want {
				t.Errorf("DetectStreamEnd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamEndTypeString(t *testing.T) {
	tests := []struct {
		typ  StreamEndType
		want string
	}{
		{StreamEndNormal, "normal"},
		{StreamEndAbnormal, "abnormal"},
		{StreamEndTimeout, "timeout"},
		{StreamEndType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.typ.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
