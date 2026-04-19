package stream

// StreamEndType classifies how a stream ended.
type StreamEndType int

const (
	// StreamEndNormal indicates the stream ended with a finish_reason or [DONE].
	StreamEndNormal StreamEndType = iota
	// StreamEndAbnormal indicates the stream ended with an error (EOF, reset)
	// without a finish_reason — the stream was cut.
	StreamEndAbnormal
	// StreamEndTimeout indicates no activity was detected within the heartbeat
	// window — the stream is probably stalled.
	StreamEndTimeout
)

// String returns a human-readable name for the stream end type.
func (t StreamEndType) String() string {
	switch t {
	case StreamEndNormal:
		return "normal"
	case StreamEndAbnormal:
		return "abnormal"
	case StreamEndTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// DetectStreamEnd classifies how a stream ended based on the last event
// and any error encountered.
//
// Classification logic:
//   - [DONE] received or finish_reason present → normal end
//   - Error (EOF, connection reset) without finish_reason → abnormal cut
//   - No finish_reason and no error → timeout (heartbeat silence)
func DetectStreamEnd(lastEvent SSEEvent, err error) StreamEndType {
	// [DONE] is the OpenAI end-of-stream signal.
	if lastEvent.Data == "[DONE]" {
		return StreamEndNormal
	}

	// finish_reason present means the model completed normally.
	if lastEvent.FinishReason != "" {
		return StreamEndNormal
	}

	// An error without a finish signal means the stream was cut.
	if err != nil {
		return StreamEndAbnormal
	}

	// No finish signal and no error: the stream went silent (timeout).
	return StreamEndTimeout
}
