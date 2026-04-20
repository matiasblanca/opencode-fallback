package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --------------------------------------------------------------------------
// FallbackTracker
// --------------------------------------------------------------------------

func TestFallbackTracker_Record(t *testing.T) {
	tracker := NewFallbackTracker(3)

	for i := 0; i < 5; i++ {
		tracker.Record(FallbackEvent{
			Reason: fmt.Sprintf("event_%d", i),
		})
	}

	events := tracker.Events()
	if len(events) != 3 {
		t.Fatalf("len = %d, want 3", len(events))
	}
	// Oldest should be evicted — first event is event_2
	if events[0].Reason != "event_2" {
		t.Errorf("first event = %q, want event_2", events[0].Reason)
	}
	if events[1].Reason != "event_3" {
		t.Errorf("second event = %q, want event_3", events[1].Reason)
	}
	if events[2].Reason != "event_4" {
		t.Errorf("third event = %q, want event_4", events[2].Reason)
	}
}

func TestFallbackTracker_Empty(t *testing.T) {
	tracker := NewFallbackTracker(10)
	events := tracker.Events()
	if len(events) != 0 {
		t.Fatalf("len = %d, want 0", len(events))
	}
}

func TestFallbackTracker_EventsCopied(t *testing.T) {
	tracker := NewFallbackTracker(10)
	tracker.Record(FallbackEvent{Reason: "original"})

	events := tracker.Events()
	events[0].Reason = "modified"

	// Original should be unchanged.
	original := tracker.Events()
	if original[0].Reason != "original" {
		t.Errorf("mutation leaked: got %q, want %q", original[0].Reason, "original")
	}
}

func TestFallbackTracker_String(t *testing.T) {
	tracker := NewFallbackTracker(5)
	tracker.Record(FallbackEvent{Reason: "a"})
	tracker.Record(FallbackEvent{Reason: "b"})

	s := tracker.String()
	if s != "FallbackTracker(2/5 events)" {
		t.Errorf("String() = %q, want %q", s, "FallbackTracker(2/5 events)")
	}
}

// --------------------------------------------------------------------------
// handleStatus
// --------------------------------------------------------------------------

func TestHandleStatus_ReturnsJSON(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.Version != "0.8.0" {
		t.Errorf("Version = %q, want %q", resp.Version, "0.8.0")
	}
	if resp.UptimeSec < 0 {
		t.Error("UptimeSec should not be negative")
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("Providers = %d, want 1", len(resp.Providers))
	}
	if resp.Providers[0].ID != "openai" {
		t.Errorf("Provider ID = %q, want %q", resp.Providers[0].ID, "openai")
	}
	if resp.Providers[0].CircuitState != "closed" {
		t.Errorf("CircuitState = %q, want %q", resp.Providers[0].CircuitState, "closed")
	}
}

func TestHandleStatus_ShowsRecentFallbacks(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	// Manually record a fallback event.
	handler.tracker.Record(FallbackEvent{
		FromProvider: "anthropic",
		FromModel:    "claude",
		Reason:       "rate_limit",
		Success:      true,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(resp.Recent) != 1 {
		t.Fatalf("Recent = %d, want 1", len(resp.Recent))
	}
	if resp.Recent[0].Reason != "rate_limit" {
		t.Errorf("Recent[0].Reason = %q, want %q", resp.Recent[0].Reason, "rate_limit")
	}
}

func TestHandleStatus_WrongMethod(t *testing.T) {
	handler := newTestHandler(&mockProvider{id: "openai"})

	// POST to /v1/status should be 404.
	req := httptest.NewRequest(http.MethodPost, "/v1/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404 for POST /v1/status", rec.Code)
	}
}
