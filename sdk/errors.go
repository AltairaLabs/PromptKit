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

	// ErrResumeHistoryUnsupported is returned by ResumeDuplex for a realtime
	// conversation whose provider cannot give a new session the persisted
	// history, so the conversation would continue without it.
	ErrResumeHistoryUnsupported = errors.New(
		"the realtime provider cannot replay the persisted conversation into a new session")

	// ErrNoStateStore is returned by Resume when no state store is configured.
	ErrNoStateStore = errors.New("no state store configured")

	// ErrPromptNotFound is returned when the specified prompt doesn't exist in the pack.
	ErrPromptNotFound = errors.New("prompt not found in pack")

	// ErrPackNotFound is returned when the pack file doesn't exist.
	ErrPackNotFound = errors.New("pack file not found")

	// ErrProviderNotDetected is returned when no provider could be auto-detected.
	ErrProviderNotDetected = errors.New("could not detect provider: no API keys found in environment")

	// ErrToolNotRegistered is returned by the conversation's tool executors
	// when the LLM calls a tool that has no handler (the model receives the
	// error's text as the tool result), and by ResolveTool and
	// ResolveToolWithArgs for a pending call whose tool has none. The error
	// text names the tool.
	ErrToolNotRegistered = errors.New("tool handler not registered")

	// ErrToolNotInPack is returned when a handler registered with
	// OnToolExecutor is called for a tool the pack does not define (the model
	// receives the error's text as the tool result). The registration methods
	// return no error, so the check happens when the tool is called.
	ErrToolNotInPack = errors.New("tool not defined in pack")

	// ErrNoWorkflow is returned when OpenWorkflow is called on a pack without a workflow section.
	ErrNoWorkflow = errors.New("pack has no workflow section")

	// ErrWorkflowClosed is returned when Send or Transition is called on a closed WorkflowConversation.
	ErrWorkflowClosed = errors.New("workflow conversation is closed")

	// ErrWorkflowTerminal is returned when Transition is called on a terminal state.
	ErrWorkflowTerminal = errors.New("workflow is in terminal state")

	// ErrMessageTooLarge is returned when a user message exceeds the configured maximum size.
	ErrMessageTooLarge = errors.New("message exceeds maximum size")

	// ErrToolCallNotAwaited is returned by ResolveTool, ResolveToolWithArgs,
	// RejectTool and SendToolResult for a call the conversation is not waiting
	// on: the turn that made it was continued or abandoned (Send started a new
	// turn), so an answer would never reach the model. ResolveTool does not
	// run the tool's handler. ResolveTool and RejectTool remove the call from
	// the pending store.
	ErrToolCallNotAwaited = errors.New("tool call is not awaited")
)

// sentinelError marks err as one of the sentinels above without changing its
// text, so errors.Is matches the sentinel while the message keeps its detail.
type sentinelError struct {
	err      error
	sentinel error
}

// markSentinel returns err marked as sentinel.
func markSentinel(err, sentinel error) error {
	return &sentinelError{err: err, sentinel: sentinel}
}

// Error returns the wrapped error's text unchanged.
func (e *sentinelError) Error() string { return e.err.Error() }

// Unwrap exposes both the sentinel and the original error.
func (e *sentinelError) Unwrap() []error { return []error{e.sentinel, e.err} }

// ValidationError represents a validation failure. Send, Stream and their
// Resume and Continue counterparts return one when a hook or guardrail denies
// the turn: it wraps the runtime's *hooks.HookDeniedError (a provider hook
// denied the call) or *providers.ValidationAbortError (a chunk hook aborted
// the stream), so errors.As still finds those.
type ValidationError struct {
	// ValidatorType is the type of validator that failed (e.g., "banned_words").
	ValidatorType string

	// Message describes what validation rule was violated.
	Message string

	// Details contains validator-specific information about the failure.
	Details map[string]any

	// cause is the runtime error the SDK wrapped, if any.
	cause error
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return "validation failed: " + e.ValidatorType + ": " + e.Message
}

// Unwrap returns the runtime error the SDK wrapped, or nil.
func (e *ValidationError) Unwrap() error {
	return e.cause
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

// PackError represents an error loading or parsing a pack file. Open and its
// variants, ValidatePack and Evaluate return one when the pack file is missing,
// unreadable, malformed or fails validation; Cause is the underlying error, so
// errors.Is(err, ErrPackNotFound) still matches a missing file.
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

// ProviderError represents an error from the LLM provider. Send, Stream and
// their Resume and Continue counterparts return one when the provider's API
// answered with an error status (*providers.ProviderHTTPError) or the
// connection failed (*providers.ProviderTransportError). Cause is that
// runtime error, so errors.As and providers.IsTransient still see it.
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

// ToolError represents an error executing a tool. ResolveTool and
// ResolveToolWithArgs set one as the ToolResolution's Error when the approved
// tool's handler fails.
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
