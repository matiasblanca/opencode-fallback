package fallback

import "fmt"

// AllProvidersExhaustedError is returned when every provider in the chain
// has failed.
type AllProvidersExhaustedError struct {
	Failures []FailureRecord
}

func (e *AllProvidersExhaustedError) Error() string {
	return fmt.Sprintf("all %d providers exhausted", len(e.Failures))
}
