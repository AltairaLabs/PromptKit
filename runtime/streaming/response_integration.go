package streaming

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// ResponseAction indicates what action to take after processing a response element.
type ResponseAction int

const (
	// ResponseActionContinue means the element was informational (e.g., interruption signal),
	// and we should continue waiting for the final response.
	ResponseActionContinue ResponseAction = iota
	// ResponseActionComplete means we received a complete response.
	ResponseActionComplete
	// ResponseActionError means an error occurred or the response was empty.
	ResponseActionError
	// ResponseActionToolCalls means the response contains tool calls that need to be executed.
	ResponseActionToolCalls
)

// String returns a human-readable representation of the action.
func (a ResponseAction) String() string {
	switch a {
	case ResponseActionContinue:
		return "continue"
	case ResponseActionComplete:
		return "complete"
	case ResponseActionError:
		return "error"
	case ResponseActionToolCalls:
		return "tool_calls"
	default:
		return "unknown"
	}
}

// ErrEmptyResponse is returned when a response element has no content.
// This typically indicates an interrupted response that wasn't properly handled.
var ErrEmptyResponse = errors.New("empty response, likely interrupted")

// ProcessResponseElement handles a response element from the pipeline, determining
// the appropriate action based on interruption signals, turn completion, and errors.
//
// This is the core state machine for duplex streaming response handling. It
// consolidates the response handling logic needed for bidirectional streaming.
//
// Returns:
//   - ResponseAction: what action to take
//   - error: any error to return (only set when action is ResponseActionError)
//
//nolint:gocognit // Complexity is acceptable for response state handling
func ProcessResponseElement(elem *stage.StreamElement, logPrefix string) (ResponseAction, error) {
	// Check for errors
	if elem.Error != nil {
		return ResponseActionError, elem.Error
	}

	// Check for interruption signals - these are informational, keep waiting
	if elem.Metadata != nil {
		// Interruption signal: provider detected user started speaking during response.
		// The partial response has been captured, now waiting for the new response.
		if interrupted, ok := elem.Metadata["interrupted"].(bool); ok && interrupted {
			logger.Debug(logPrefix + ": response interrupted, waiting for new response")
			return ResponseActionContinue, nil
		}

		// Interrupted turn complete: Empty turnComplete after interruption.
		// This is just the provider closing the interrupted turn, not the final response.
		if itc, ok := elem.Metadata["interrupted_turn_complete"].(bool); ok && itc {
			logger.Debug(logPrefix + ": interrupted turn complete, waiting for real response")
			return ResponseActionContinue, nil
		}
	}

	// Check for turn completion (EndOfStream from DuplexProviderStage)
	if elem.EndOfStream {
		logger.Debug(logPrefix+": EndOfStream received",
			"hasMessage", elem.Message != nil,
			"hasText", elem.Text != nil)

		// Check for empty response - shouldn't happen with proper interruption handling,
		// but serves as a safety net. Treat as retriable error.
		// A response with tool calls but no content is valid (model is requesting tool execution)
		hasContent := elem.Message != nil && (elem.Message.Content != "" || len(elem.Message.Parts) > 0)
		hasToolCalls := elem.Message != nil && len(elem.Message.ToolCalls) > 0
		if !hasContent && !hasToolCalls {
			logger.Debug(logPrefix + ": empty response, treating as retriable error")
			return ResponseActionError, ErrEmptyResponse
		}

		// If response contains tool calls, signal that tools need to be executed
		// The caller will execute tools and send results back, then continue waiting
		if hasToolCalls {
			logger.Debug(logPrefix+": received tool call response, needs execution",
				"tool_count", len(elem.Message.ToolCalls))
			return ResponseActionToolCalls, nil
		}

		return ResponseActionComplete, nil
	}

	// Element doesn't require action (e.g., streaming text/audio chunk)
	return ResponseActionContinue, nil
}
