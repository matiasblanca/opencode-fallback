package transform

import (
	"testing"
)

func TestComputeBillingHeader(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "reference test from cch.test.ts",
			message: "hello world test message",
			want:    "cc_version=2.1.87.6ff; cc_entrypoint=sdk-cli; cch=4ffc3;",
		},
		{
			name:    "empty message",
			message: "",
			want:    "cc_version=2.1.87." + computeVersionSuffix("") + "; cc_entrypoint=sdk-cli; cch=" + computeCCH("") + ";",
		},
		{
			name:    "short message (less than 21 chars)",
			message: "hi",
			want:    "cc_version=2.1.87." + computeVersionSuffix("hi") + "; cc_entrypoint=sdk-cli; cch=" + computeCCH("hi") + ";",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeBillingHeader(tt.message)
			if got != tt.want {
				t.Errorf("ComputeBillingHeader(%q) =\n  %q\nwant:\n  %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestComputeCCH_ReferenceCase(t *testing.T) {
	// From cch.test.ts: SHA-256("hello world test message") starts with "4ffc3"
	got := computeCCH("hello world test message")
	if got != "4ffc3" {
		t.Errorf("computeCCH(\"hello world test message\") = %q, want %q", got, "4ffc3")
	}
}

func TestComputeVersionSuffix_ReferenceCase(t *testing.T) {
	// From cch.test.ts: suffix for "hello world test message" is "6ff"
	got := computeVersionSuffix("hello world test message")
	if got != "6ff" {
		t.Errorf("computeVersionSuffix(\"hello world test message\") = %q, want %q", got, "6ff")
	}
}

func TestExtractChars(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "full message with all positions available",
			message: "hello world test message",
			want:    "oos", // positions [4]=o, [7]=o, [20]=s
		},
		{
			name:    "short message with fallbacks",
			message: "hi",
			want:    "000", // all positions out of bounds
		},
		{
			name:    "partial positions available",
			message: "hello w",
			want:    "o00", // [4]=o, [7]=out of bounds(7 chars = indices 0-6), [20]=out of bounds
		},
		{
			name:    "empty message",
			message: "",
			want:    "000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractChars(tt.message)
			if got != tt.want {
				t.Errorf("extractChars(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestComputeBillingHeader_ExactReferenceOutput(t *testing.T) {
	// This is the EXACT test case from cch.test.ts:35-44.
	got := ComputeBillingHeader("hello world test message")
	want := "cc_version=2.1.87.6ff; cc_entrypoint=sdk-cli; cch=4ffc3;"
	if got != want {
		t.Errorf("billing header mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}
