// Package hooks provides synchronous interception points for provider calls,
// tool execution, and session lifecycle in the PromptKit runtime.
package hooks

import "fmt"

// HookDeniedError is returned when a hook denies an operation.
//
// The provider stage returns it today, and sets HookName to the fixed string
// "provider_hook" rather than the denying hook's own Name().
type HookDeniedError struct {
	HookName string
	HookType string // "provider_before" or "provider_after"
	Reason   string
	Metadata map[string]any
}

func (e *HookDeniedError) Error() string {
	return fmt.Sprintf("hook %q (%s) denied: %s", e.HookName, e.HookType, e.Reason)
}
