// Package transform — cch.go computes the CCH (Claude Code Hash) billing
// header used for Anthropic OAuth authentication.
//
// The billing header tells Anthropic's backend that the request comes from
// Claude Code, enabling subscription-based billing.
package transform

import (
	"crypto/sha256"
	"fmt"
)

const (
	// cchSalt is the salt used in the version suffix computation.
	cchSalt = "59cf53e54c78"

	// cchVersion is the Claude Code version we impersonate.
	cchVersion = "2.1.87"

	// cchEntrypoint identifies the entry point type.
	cchEntrypoint = "sdk-cli"

	// charPositions are the character indices extracted from the message.
	// If a position is out of bounds, '0' is used as fallback.
	charPosition0 = 4
	charPosition1 = 7
	charPosition2 = 20
)

// ComputeBillingHeader builds the full billing header value from the first
// user message text.
//
// Format: "cc_version=2.1.87.<suffix>; cc_entrypoint=sdk-cli; cch=<hash>;"
//
// The CCH is the first 5 hex chars of SHA-256 of the message text.
// The suffix is the first 3 hex chars of SHA-256 of (salt + chars + version).
func ComputeBillingHeader(firstUserMessage string) string {
	cch := computeCCH(firstUserMessage)
	suffix := computeVersionSuffix(firstUserMessage)

	return fmt.Sprintf("cc_version=%s.%s; cc_entrypoint=%s; cch=%s;",
		cchVersion, suffix, cchEntrypoint, cch)
}

// computeCCH computes the first 5 hex chars of SHA-256 of the message.
func computeCCH(message string) string {
	hash := sha256.Sum256([]byte(message))
	return fmt.Sprintf("%x", hash[:])[:5]
}

// computeVersionSuffix computes the first 3 hex chars of
// SHA-256(salt + chars_at_positions + version).
func computeVersionSuffix(message string) string {
	chars := extractChars(message)
	input := cchSalt + chars + cchVersion
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash[:])[:3]
}

// extractChars extracts characters at positions [4, 7, 20] from the message.
// Uses '0' as fallback for out-of-bounds positions.
func extractChars(message string) string {
	positions := []int{charPosition0, charPosition1, charPosition2}
	runes := []rune(message)

	result := make([]byte, len(positions))
	for i, pos := range positions {
		if pos < len(runes) {
			result[i] = byte(runes[pos])
		} else {
			result[i] = '0'
		}
	}

	return string(result)
}
