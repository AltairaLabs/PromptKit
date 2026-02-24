// Package hooks provides synchronous interception points for provider calls,
// tool execution, and session lifecycle in the PromptKit runtime.
package hooks

import "fmt"

// HookDeniedError is returned when a hook denies an operation.
type HookDeniedError struct {
	HookName string
	HookType string // "provider_before", "provider_after", "chunk", "tool_before", "tool_after"
	Reason   string
	Metadata map[string]any
}

func (e *HookDeniedError) Error() string {
	return fmt.Sprintf("hook %q (%s) denied: %s", e.HookName, e.HookType, e.Reason)
}
