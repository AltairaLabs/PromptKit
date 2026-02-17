package sdk

import (
	"errors"
	"strconv"
)

// Sentinel errors for common failure cases.
var (
	// ErrConversationClosed is returned when Send or Stream is called on a closed conversation.
	ErrConversationClosed = errors.New("conversation is closed")

	// ErrConversationNotFound is returned by Resume when the conversation ID doesn't exist.
	ErrConversationNotFound = errors.New("conversation not found")

	// ErrNoStateStore is returned by Resume when no state store is configured.
	ErrNoStateStore = errors.New("no state store configured")

	// ErrPromptNotFound is returned when the specified prompt doesn't exist in the pack.
	ErrPromptNotFound = errors.New("prompt not found in pack")

	// ErrPackNotFound is returned when the pack file doesn't exist.
	ErrPackNotFound = errors.New("pack file not found")

	// ErrProviderNotDetected is returned when no provider could be auto-detected.
	ErrProviderNotDetected = errors.New("could not detect provider: no API keys found in environment")

	// ErrToolNotRegistered is returned when the LLM calls a tool that has no handler.
	ErrToolNotRegistered = errors.New("tool handler not registered")

	// ErrToolNotInPack is returned when trying to register a handler for a tool not in the pack.
	ErrToolNotInPack = errors.New("tool not defined in pack")

	// ErrNoWorkflow is returned when OpenWorkflow is called on a pack without a workflow section.
	ErrNoWorkflow = errors.New("pack has no workflow section")

	// ErrWorkflowClosed is returned when Send or Transition is called on a closed WorkflowConversation.
	ErrWorkflowClosed = errors.New("workflow conversation is closed")

	// ErrWorkflowTerminal is returned when Transition is called on a terminal state.
	ErrWorkflowTerminal = errors.New("workflow is in terminal state")
)

// ValidationError represents a validation failure.
type ValidationError struct {
	// ValidatorType is the type of validator that failed (e.g., "banned_words").
	ValidatorType string

	// Message describes what validation rule was violated.
	Message string

	// Details contains validator-specific information about the failure.
	Details map[string]any
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return "validation failed: " + e.ValidatorType + ": " + e.Message
}

// AsValidationError checks if an error is a ValidationError and returns it.
//
//	resp, err := conv.Send(ctx, message)
//	if err != nil {
//	    if vErr, ok := sdk.AsValidationError(err); ok {
//	        fmt.Printf("Validation failed: %s\n", vErr.ValidatorType)
//	    }
//	}
func AsValidationError(err error) (*ValidationError, bool) {
	var vErr *ValidationError
	if errors.As(err, &vErr) {
		return vErr, true
	}
	return nil, false
}

// PackError represents an error loading or parsing a pack file.
type PackError struct {
	// Path is the pack file path.
	Path string

	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *PackError) Error() string {
	return "failed to load pack " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *PackError) Unwrap() error {
	return e.Cause
}

// ProviderError represents an error from the LLM provider.
type ProviderError struct {
	// Provider name (e.g., "openai", "anthropic").
	Provider string

	// StatusCode is the HTTP status code if available.
	StatusCode int

	// Message is the error message from the provider.
	Message string

	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	msg := "provider " + e.Provider + " error"
	if e.StatusCode > 0 {
		msg += " (" + strconv.Itoa(e.StatusCode) + ")"
	}
	return msg + ": " + e.Message
}

// Unwrap returns the underlying error.
func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// ToolError represents an error executing a tool.
type ToolError struct {
	// ToolName is the name of the tool that failed.
	ToolName string

	// Cause is the underlying error from the tool handler.
	Cause error
}

// Error implements the error interface.
func (e *ToolError) Error() string {
	return "tool " + e.ToolName + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *ToolError) Unwrap() error {
	return e.Cause
}
