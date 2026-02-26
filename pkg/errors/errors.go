// Package errors provides standardized error types for use across PromptKit modules.
//
// ContextualError is the base error type that captures component, operation, and
// optional status code and details. It implements the error and Unwrap interfaces
// for seamless integration with Go's errors package.
//
// Usage:
//
//	err := errors.New("sdk", "OpenConversation", someErr)
//	err = err.WithStatusCode(404).WithDetails(map[string]any{"pack": "my-pack"})
package errors

import "fmt"

// ContextualError is a structured error type that provides consistent context
// about where and why an error occurred across PromptKit modules.
type ContextualError struct {
	// Component identifies the module that produced the error (e.g. "sdk", "runtime", "arena").
	Component string

	// Operation describes what was being done when the error occurred.
	Operation string

	// StatusCode is an optional HTTP or application-level status code.
	StatusCode int

	// Details holds optional structured metadata about the error.
	Details map[string]any

	// Cause is the underlying error, if any.
	Cause error
}

// New creates a ContextualError with the given component, operation, and cause.
func New(component, operation string, cause error) *ContextualError {
	return &ContextualError{
		Component: component,
		Operation: operation,
		Cause:     cause,
	}
}

// Error returns a human-readable representation of the error.
func (e *ContextualError) Error() string {
	base := fmt.Sprintf("[%s] %s", e.Component, e.Operation)

	if e.StatusCode != 0 {
		base += fmt.Sprintf(" (status %d)", e.StatusCode)
	}

	if e.Cause != nil {
		base += ": " + e.Cause.Error()
	}

	return base
}

// Unwrap returns the underlying cause, enabling use with errors.Is and errors.As.
func (e *ContextualError) Unwrap() error {
	return e.Cause
}

// WithStatusCode returns a copy of the error with the given status code set.
func (e *ContextualError) WithStatusCode(code int) *ContextualError {
	e.StatusCode = code
	return e
}

// WithDetails returns a copy of the error with the given details map set.
func (e *ContextualError) WithDetails(details map[string]any) *ContextualError {
	e.Details = details
	return e
}
