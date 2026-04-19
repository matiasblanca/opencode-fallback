package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/matiasblanca/opencode-fallback/internal/fallback"
	"github.com/matiasblanca/opencode-fallback/internal/provider"
	"github.com/matiasblanca/opencode-fallback/internal/stream"
)

// Handler is the main HTTP handler for the proxy.
// It receives OpenAI-compatible requests and dispatches them through the
// fallback chain.
type Handler struct {
	selector *fallback.ChainSelector
	logger   *slog.Logger
}

// NewHandler creates a proxy handler with the given chain selector.
func NewHandler(selector *fallback.ChainSelector, logger *slog.Logger) *Handler {
	return &Handler{
		selector: selector,
		logger:   logger,
	}
}

// ServeHTTP handles incoming requests.
// Only POST /v1/chat/completions is supported — everything else returns 404.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read request body", "error", err)
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	req, err := parseRequest(body, r.Header)
	if err != nil {
		h.logger.Error("failed to parse request", "error", err)
		writeOpenAIError(w, http.StatusBadRequest, "invalid request format")
		return
	}

	h.logger.Debug("request received",
		"model", req.Model,
		"stream", req.Stream,
	)

	ctx := r.Context()
	chain := h.selector.SelectChain(req.Model)
	result := chain.Execute(ctx, req)

	// Log failures.
	for _, f := range result.Failures {
		h.logger.Info("fallback triggered",
			"from", f.ProviderID+"/"+f.ModelID,
			"reason", f.Reason,
			"duration", f.Duration,
		)
	}

	if !result.Success {
		h.logger.Error("all providers exhausted",
			"failures_count", len(result.Failures),
		)
		writeOpenAIError(w, http.StatusBadGateway, "all providers unavailable")
		return
	}

	h.logger.Info("request completed",
		"provider", result.Provider,
		"model", result.ModelID,
		"fallbacks", len(result.Failures),
	)

	if result.Stream != nil {
		h.streamToClient(w, result.Stream)
		return
	}

	// Non-streaming response.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Response.StatusCode)
	w.Write(result.Response.Body)
}

// streamToClient forwards SSE events from the parser to the HTTP response.
// For v0.1, this is a simplified implementation that reads all events and
// writes them as SSE to the client.
func (h *Handler) streamToClient(w http.ResponseWriter, parser *stream.SSEParser) {
	defer parser.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	for {
		ev, err := parser.Next()
		if err != nil {
			break
		}

		if ev.IsKeepAlive {
			fmt.Fprintf(w, "%s\n\n", ev.Raw)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", ev.Data)
		}

		if canFlush {
			flusher.Flush()
		}
	}
}

// parseRequest extracts a ProxyRequest from the raw body and headers.
func parseRequest(body []byte, headers http.Header) (*provider.ProxyRequest, error) {
	var parsed struct {
		Model       string              `json:"model"`
		Messages    []provider.Message  `json:"messages"`
		Stream      bool                `json:"stream"`
		Temperature *float64            `json:"temperature,omitempty"`
		MaxTokens   *int                `json:"max_tokens,omitempty"`
		Tools       []provider.Tool     `json:"tools,omitempty"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}

	if parsed.Model == "" {
		return nil, fmt.Errorf("model field is required")
	}

	return &provider.ProxyRequest{
		Model:       parsed.Model,
		Messages:    parsed.Messages,
		Stream:      parsed.Stream,
		Temperature: parsed.Temperature,
		MaxTokens:   parsed.MaxTokens,
		Tools:       parsed.Tools,
		RawBody:     body,
		Headers:     headers,
	}, nil
}

// writeOpenAIError writes an error response in OpenAI-compatible format.
// Coding agents expect this format — never send raw errors.
func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "proxy_error",
			"code":    status,
		},
	})
}
