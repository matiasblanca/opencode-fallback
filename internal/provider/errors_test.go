package provider

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// IsOverflow
// --------------------------------------------------------------------------

func TestIsOverflow(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"prompt too long", `{"error":{"message":"prompt is too long"}}`, true},
		{"context length exceeded", `{"error":{"message":"context_length_exceeded"}}`, true},
		{"exceeds limit with number", `{"error":{"message":"exceeds the limit of 200000"}}`, true},
		{"input too long bedrock", `{"error":{"message":"input is too long for requested model"}}`, true},
		{"request entity too large", `request entity too large`, true},
		{"exceeds context window", `{"error":{"message":"This request exceeds the context window"}}`, true},
		{"maximum context length", `{"error":{"message":"maximum context length is 128000 tokens"}}`, true},
		{"reduce length", `{"error":{"message":"Please reduce the length of the messages"}}`, true},
		{"normal rate limit", `{"error":{"message":"rate limit exceeded"}}`, false},
		{"normal error", `{"error":{"message":"internal server error"}}`, false},
		{"empty body", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsOverflow([]byte(tt.body))
			if got != tt.want {
				t.Errorf("IsOverflow(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestClassifyAnthropicError_ContextOverflow(t *testing.T) {
	body := []byte(`{"error":{"message":"prompt is too long for model"}}`)
	got := ClassifyAnthropicError(400, http.Header{}, body)
	if got.Reason != "context_overflow" {
		t.Errorf("Reason = %q, want context_overflow", got.Reason)
	}
	if got.Type != ErrorFatal {
		t.Errorf("Type = %v, want ErrorFatal", got.Type)
	}
}

func TestClassifyAnthropicError_HTTP413(t *testing.T) {
	got := ClassifyAnthropicError(413, http.Header{}, nil)
	if got.Reason != "context_overflow" {
		t.Errorf("Reason = %q, want context_overflow", got.Reason)
	}
	if got.Type != ErrorFatal {
		t.Errorf("Type = %v, want ErrorFatal", got.Type)
	}
}

func TestClassifyGenericOpenAIError_ContextOverflow(t *testing.T) {
	body := []byte(`{"error":{"message":"context_length_exceeded"}}`)
	got := ClassifyGenericOpenAIError(400, http.Header{}, body)
	if got.Reason != "context_overflow" {
		t.Errorf("Reason = %q, want context_overflow", got.Reason)
	}
	if got.Type != ErrorFatal {
		t.Errorf("Type = %v, want ErrorFatal", got.Type)
	}
}

func TestClassifyGenericOpenAIError_HTTP413(t *testing.T) {
	got := ClassifyGenericOpenAIError(413, http.Header{}, nil)
	if got.Reason != "context_overflow" {
		t.Errorf("Reason = %q, want context_overflow", got.Reason)
	}
}

func TestClassifyDeepSeekError_ContextOverflow(t *testing.T) {
	body := []byte(`{"error":{"message":"exceeds the limit of 65536"}}`)
	got := ClassifyDeepSeekError(400, http.Header{}, body)
	if got.Reason != "context_overflow" {
		t.Errorf("Reason = %q, want context_overflow", got.Reason)
	}
}

// --------------------------------------------------------------------------
// IsQuotaExhausted
// --------------------------------------------------------------------------

func TestIsQuotaExhausted(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"openai quota", `{"error":{"message":"You exceeded your current quota"}}`, true},
		{"billing limit", `{"error":{"message":"billing hard limit reached"}}`, true},
		{"insufficient quota", `{"error":{"code":"insufficient_quota"}}`, true},
		{"spending limit", `{"error":{"message":"spending limit exceeded"}}`, true},
		{"credit balance", `{"error":{"message":"credit balance is too low"}}`, true},
		{"monthly limit", `{"error":{"message":"monthly limit reached"}}`, true},
		{"normal rate limit", `{"error":{"message":"rate limit exceeded"}}`, false},
		{"empty body", ``, false},
		{"unrelated error", `{"error":{"message":"server error"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsQuotaExhausted([]byte(tt.body))
			if got != tt.want {
				t.Errorf("IsQuotaExhausted(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestClassifyGenericOpenAIError_QuotaExhausted(t *testing.T) {
	body := []byte(`{"error":{"message":"You exceeded your current quota","type":"insufficient_quota"}}`)
	got := ClassifyGenericOpenAIError(429, nil, body)
	if got.Type != ErrorFatal {
		t.Errorf("Type = %v, want ErrorFatal", got.Type)
	}
	if got.Reason != "quota_exhausted" {
		t.Errorf("Reason = %q, want %q", got.Reason, "quota_exhausted")
	}
}

func TestClassifyAnthropicError_QuotaExhausted(t *testing.T) {
	body := []byte(`{"error":{"message":"billing hard limit reached"}}`)
	got := ClassifyAnthropicError(429, nil, body)
	if got.Type != ErrorFatal {
		t.Errorf("Type = %v, want ErrorFatal", got.Type)
	}
	if got.Reason != "quota_exhausted" {
		t.Errorf("Reason = %q, want %q", got.Reason, "quota_exhausted")
	}
}

// --------------------------------------------------------------------------
// ErrorClassification methods
// --------------------------------------------------------------------------

func TestErrorClassificationIsRetriable(t *testing.T) {
	c := ErrorClassification{Type: ErrorRetriable}
	if !c.IsRetriable() {
		t.Error("IsRetriable() = false for ErrorRetriable, want true")
	}
	if c.IsFatal() {
		t.Error("IsFatal() = true for ErrorRetriable, want false")
	}
}

func TestErrorClassificationIsFatal(t *testing.T) {
	c := ErrorClassification{Type: ErrorFatal}
	if !c.IsFatal() {
		t.Error("IsFatal() = false for ErrorFatal, want true")
	}
	if c.IsRetriable() {
		t.Error("IsRetriable() = true for ErrorFatal, want false")
	}
}

// --------------------------------------------------------------------------
// ClassifyAnthropicError
// --------------------------------------------------------------------------

func TestClassifyAnthropicError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    http.Header
		body       []byte
		wantType   ErrorType
		wantReason string
	}{
		{
			name:       "429 rate limit",
			status:     429,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit",
		},
		{
			name:   "429 rate limit with retry-after",
			status: 429,
			headers: http.Header{
				"Retry-After": []string{"30"},
			},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit",
		},
		{
			name:       "529 overloaded",
			status:     529,
			headers:    http.Header{},
			body:       []byte(`{"error":{"type":"overloaded_error","message":"Overloaded"}}`),
			wantType:   ErrorRetriable,
			wantReason: "overloaded",
		},
		{
			name:       "401 auth error",
			status:     401,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "auth",
		},
		{
			name:       "403 forbidden",
			status:     403,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "auth",
		},
		{
			name:       "500 server error",
			status:     500,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "502 bad gateway",
			status:     502,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "400 bad request",
			status:     400,
			headers:    http.Header{},
			body:       []byte(`{"error":{"type":"invalid_request_error"}}`),
			wantType:   ErrorFatal,
			wantReason: "client_error",
		},
		{
			name:       "404 not found",
			status:     404,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "client_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyAnthropicError(tt.status, tt.headers, tt.body)
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

func TestClassifyAnthropicErrorRetryAfterParsing(t *testing.T) {
	headers := http.Header{
		"Retry-After": []string{"45"},
	}
	got := ClassifyAnthropicError(429, headers, nil)
	if got.RetryAfter != 45*time.Second {
		t.Errorf("RetryAfter = %v, want 45s", got.RetryAfter)
	}
}

// --------------------------------------------------------------------------
// ClassifyOpenAIError
// --------------------------------------------------------------------------

func TestClassifyOpenAIError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    http.Header
		body       []byte
		wantType   ErrorType
		wantReason string
	}{
		{
			name:       "429 rate limit",
			status:     429,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit",
		},
		{
			name:   "429 tokens exhausted",
			status: 429,
			headers: http.Header{
				"X-Ratelimit-Remaining-Tokens": []string{"0"},
			},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit_tokens_exhausted",
		},
		{
			name:       "401 auth",
			status:     401,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "auth",
		},
		{
			name:       "500 server error",
			status:     500,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "503 service unavailable",
			status:     503,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "400 bad request",
			status:     400,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "client_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyOpenAIError(tt.status, tt.headers, tt.body)
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ClassifyGenericOpenAIError
// --------------------------------------------------------------------------

func TestClassifyGenericOpenAIError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    http.Header
		body       []byte
		wantType   ErrorType
		wantReason string
	}{
		{
			name:       "429 rate limit",
			status:     429,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit",
		},
		{
			name:   "429 tokens exhausted",
			status: 429,
			headers: http.Header{
				"X-Ratelimit-Remaining-Tokens": []string{"0"},
			},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit_tokens_exhausted",
		},
		{
			name:       "529 overloaded",
			status:     529,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "overloaded",
		},
		{
			name:       "401 auth",
			status:     401,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "auth",
		},
		{
			name:       "403 forbidden",
			status:     403,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "auth",
		},
		{
			name:       "404 not found",
			status:     404,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "model_not_found",
		},
		{
			name:       "500 server error",
			status:     500,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "503 service unavailable",
			status:     503,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "400 bad request",
			status:     400,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "client_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGenericOpenAIError(tt.status, tt.headers, tt.body)
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

func TestClassifyGenericOpenAIError_RetryAfter(t *testing.T) {
	headers := http.Header{"Retry-After": []string{"60"}}
	got := ClassifyGenericOpenAIError(429, headers, nil)
	if got.RetryAfter != 60*time.Second {
		t.Errorf("RetryAfter = %v, want 60s", got.RetryAfter)
	}
}

// --------------------------------------------------------------------------
// ClassifyDeepSeekError
// --------------------------------------------------------------------------

func TestClassifyDeepSeekError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    http.Header
		body       []byte
		wantType   ErrorType
		wantReason string
	}{
		{
			name:       "429 rate limit",
			status:     429,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "rate_limit",
		},
		{
			name:       "401 auth",
			status:     401,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "auth",
		},
		{
			name:       "500 server error",
			status:     500,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "502 bad gateway",
			status:     502,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorRetriable,
			wantReason: "server_error",
		},
		{
			name:       "400 bad request",
			status:     400,
			headers:    http.Header{},
			body:       nil,
			wantType:   ErrorFatal,
			wantReason: "client_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyDeepSeekError(tt.status, tt.headers, tt.body)
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ClassifyTransportError
// --------------------------------------------------------------------------

func TestClassifyTransportError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantMatch  bool
		wantStatus int
		wantReason string
	}{
		{
			name:       "connection refused",
			err:        errors.New("dial tcp 127.0.0.1:11434: connection refused"),
			wantMatch:  true,
			wantStatus: 503,
			wantReason: "network",
		},
		{
			name:       "no such host",
			err:        errors.New("dial tcp: lookup api.example.com: no such host"),
			wantMatch:  true,
			wantStatus: 503,
			wantReason: "network",
		},
		{
			name:       "network unreachable",
			err:        errors.New("dial tcp: network is unreachable"),
			wantMatch:  true,
			wantStatus: 503,
			wantReason: "network",
		},
		{
			name:       "i/o timeout",
			err:        errors.New("dial tcp 1.2.3.4:443: i/o timeout"),
			wantMatch:  true,
			wantStatus: 504,
			wantReason: "network",
		},
		{
			name:       "tls handshake timeout",
			err:        errors.New("net/http: TLS handshake timeout"),
			wantMatch:  true,
			wantStatus: 504,
			wantReason: "network",
		},
		{
			name:       "connection reset by peer",
			err:        errors.New("read tcp 10.0.0.1:443: connection reset by peer"),
			wantMatch:  true,
			wantStatus: 503,
			wantReason: "network",
		},
		{
			name:       "context deadline exceeded",
			err:        errors.New("context deadline exceeded"),
			wantMatch:  true,
			wantStatus: 504,
			wantReason: "network",
		},
		{
			name:      "nil error",
			err:       nil,
			wantMatch: false,
		},
		{
			name:      "non-transport error",
			err:       errors.New("invalid json body"),
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ClassifyTransportError(tt.err)
			if ok != tt.wantMatch {
				t.Errorf("match = %v, want %v", ok, tt.wantMatch)
			}
			if !tt.wantMatch {
				return
			}
			if got.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.wantStatus)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if !got.IsRetriable() {
				t.Error("transport error should be retriable")
			}
			if got.RawError != tt.err {
				t.Error("RawError should be the original error")
			}
		})
	}
}

// --------------------------------------------------------------------------
// parseRetryAfter
// --------------------------------------------------------------------------

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 0},
		{"30 seconds", "30", 30 * time.Second},
		{"120 seconds", "120", 120 * time.Second},
		{"non-numeric", "not-a-number", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.value)
			if got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ErrorType.String
// --------------------------------------------------------------------------

func TestErrorTypeString(t *testing.T) {
	tests := []struct {
		errorType ErrorType
		want      string
	}{
		{ErrorRetriable, "retriable"},
		{ErrorFatal, "fatal"},
		{ErrorType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.errorType.String(); got != tt.want {
				t.Errorf("ErrorType(%d).String() = %q, want %q", tt.errorType, got, tt.want)
			}
		})
	}
}
