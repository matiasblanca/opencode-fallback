package stream

import (
	"strings"
)

// CheckpointBuffer accumulates stream content for recovery purposes.
//
// During streaming, each SSEEvent is appended to the buffer. If the stream
// is cut abnormally, the buffer's accumulated text can be used to build a
// continuation request to a fallback provider.
//
// Not safe for concurrent use — callers must synchronize access externally
// if needed (typically the fallback chain holds the buffer in a single
// goroutine).
type CheckpointBuffer struct {
	tokens   []string
	fullText strings.Builder
	events   []SSEEvent
}

// NewCheckpointBuffer creates an empty buffer.
func NewCheckpointBuffer() *CheckpointBuffer {
	return &CheckpointBuffer{}
}

// Append adds an event to the buffer. Content deltas are accumulated into
// the full text. Keep-alive events are recorded but do not add content.
func (b *CheckpointBuffer) Append(event SSEEvent) {
	b.events = append(b.events, event)
	if event.ContentDelta != "" {
		b.tokens = append(b.tokens, event.ContentDelta)
		b.fullText.WriteString(event.ContentDelta)
	}
}

// Text returns the complete accumulated text from all content deltas.
func (b *CheckpointBuffer) Text() string {
	return b.fullText.String()
}

// TokenCount returns the number of content chunks received (approximate
// token count).
func (b *CheckpointBuffer) TokenCount() int {
	return len(b.tokens)
}

// IsEmpty reports whether no content has been accumulated yet.
func (b *CheckpointBuffer) IsEmpty() bool {
	return b.fullText.Len() == 0
}

// EventCount returns the total number of events recorded (including
// keep-alive events with no content).
func (b *CheckpointBuffer) EventCount() int {
	return len(b.events)
}

// Events returns a copy of all recorded events.
func (b *CheckpointBuffer) Events() []SSEEvent {
	cp := make([]SSEEvent, len(b.events))
	copy(cp, b.events)
	return cp
}
