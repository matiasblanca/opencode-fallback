package provider

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrorType classifies an error as retriable or fatal.
type ErrorType int

const (
	// ErrorRetriable indicates the error can be retried with another provider.
	ErrorRetriable ErrorType = iota
	// ErrorFatal indicates the error should not be retried (e.g. auth failure).
	ErrorFatal
)

// String returns a human-readable name for the error type.
func (t ErrorType) String() string {
	switch t {
	case ErrorRetriable:
		return "retriable"
	case ErrorFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// ErrorClassification is the result of classifying an HTTP error response
// from an LLM provider. It tells the fallback chain whether to try the next
// provider or give up.
type ErrorClassification struct {
	// Type is either ErrorRetriable or ErrorFatal.
	Type ErrorType
	// Reason is a short machine-readable reason string.
	// Values: "rate_limit", "rate_limit_tokens_exhausted", "overloaded",
	// "auth", "server_error", "network", "client_error".
	Reason string
	// RetryAfter is the duration the provider suggests waiting before retry.
	// Zero if the provider did not include a Retry-After header.
	RetryAfter time.Duration
	// StatusCode is the HTTP status code from the provider, or a synthetic
	// code (503/504) for transport errors.
	StatusCode int
	// RawError is the original Go error, if the classification came from a
	// transport-level failure rather than an HTTP response.
	RawError error
}

// IsRetriable reports whether the error can be retried with another provider.
func (c ErrorClassification) IsRetriable() bool { return c.Type == ErrorRetriable }

// IsFatal reports whether the error should not be retried.
func (c ErrorClassification) IsFatal() bool { return c.Type == ErrorFatal }

// --------------------------------------------------------------------------
// Anthropic error classification
// --------------------------------------------------------------------------

// ClassifyAnthropicError classifies an HTTP error response from the Anthropic
// Messages API.
//
// Classification rules (from architecture doc §7.1):
//   - 429: retriable (rate limit), with optional Retry-After header
//   - 529: retriable (overloaded)
//   - 401/403: fatal (auth)
//   - 500+: retriable (server error)
//   - other 4xx: fatal (client error)
func ClassifyAnthropicError(status int, headers http.Header, body []byte) ErrorClassification {
	switch {
	case status == 429:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "rate_limit",
			RetryAfter: parseRetryAfter(headers.Get("Retry-After")),
			StatusCode: status,
		}
	case status == 529:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "overloaded",
			StatusCode: status,
		}
	case status == 401 || status == 403:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "auth",
			StatusCode: status,
		}
	case status >= 500:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "server_error",
			StatusCode: status,
		}
	default:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "client_error",
			StatusCode: status,
		}
	}
}

// --------------------------------------------------------------------------
// OpenAI error classification
// --------------------------------------------------------------------------

// ClassifyOpenAIError classifies an HTTP error response from the OpenAI API.
//
// Classification rules (from architecture doc §7.1):
//   - 429: retriable (rate limit); checks X-Ratelimit-Remaining-Tokens header
//   - 401: fatal (auth)
//   - 500+: retriable (server error)
//   - other 4xx: fatal (client error)
func ClassifyOpenAIError(status int, headers http.Header, body []byte) ErrorClassification {
	switch {
	case status == 429:
		reason := "rate_limit"
		if headers.Get("X-Ratelimit-Remaining-Tokens") == "0" {
			reason = "rate_limit_tokens_exhausted"
		}
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     reason,
			StatusCode: status,
		}
	case status == 401:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "auth",
			StatusCode: status,
		}
	case status >= 500:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "server_error",
			StatusCode: status,
		}
	default:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "client_error",
			StatusCode: status,
		}
	}
}

// --------------------------------------------------------------------------
// DeepSeek error classification
// --------------------------------------------------------------------------

// ClassifyDeepSeekError classifies an HTTP error response from the DeepSeek
// API. DeepSeek uses the OpenAI-compatible format so the classification is
// similar.
//
// Classification rules (from architecture doc §7.1):
//   - 429: retriable (rate limit)
//   - 401: fatal (auth)
//   - 500+: retriable (server error)
//   - other 4xx: fatal (client error)
func ClassifyDeepSeekError(status int, headers http.Header, body []byte) ErrorClassification {
	switch {
	case status == 429:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "rate_limit",
			StatusCode: status,
		}
	case status == 401:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "auth",
			StatusCode: status,
		}
	case status >= 500:
		return ErrorClassification{
			Type:       ErrorRetriable,
			Reason:     "server_error",
			StatusCode: status,
		}
	default:
		return ErrorClassification{
			Type:       ErrorFatal,
			Reason:     "client_error",
			StatusCode: status,
		}
	}
}

// --------------------------------------------------------------------------
// Transport error classification
// --------------------------------------------------------------------------

// transportErrorPatterns are regex patterns that detect Go network errors.
// Adapted from the Manifest proxy-transport.ts pattern:
//
//	/(fetch failed|timeout|econnrefused|...)/i
var transportErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)connection refused`),
	regexp.MustCompile(`(?i)no such host`),
	regexp.MustCompile(`(?i)network is unreachable`),
	regexp.MustCompile(`(?i)i/o timeout`),
	regexp.MustCompile(`(?i)tls handshake timeout`),
	regexp.MustCompile(`(?i)connection reset by peer`),
	regexp.MustCompile(`(?i)context deadline exceeded`),
}

// ClassifyTransportError checks whether a Go error is a network-level
// transport failure. If it matches, it returns an ErrorClassification with
// a synthetic status code (503 for connectivity, 504 for timeouts) and
// ok=true. If the error is not a transport error, it returns ok=false.
func ClassifyTransportError(err error) (ErrorClassification, bool) {
	if err == nil {
		return ErrorClassification{}, false
	}

	errStr := err.Error()
	for _, pattern := range transportErrorPatterns {
		if pattern.MatchString(errStr) {
			statusCode := 503 // Service Unavailable
			if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
				statusCode = 504 // Gateway Timeout
			}
			return ErrorClassification{
				Type:       ErrorRetriable,
				Reason:     "network",
				StatusCode: statusCode,
				RawError:   err,
			}, true
		}
	}

	return ErrorClassification{}, false
}

// parseRetryAfter parses the Retry-After header value.
// It supports integer seconds. Returns 0 if the value is empty or invalid.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	return 0
}
