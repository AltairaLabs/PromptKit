package engine

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// Audio configuration constants
	geminiAudioBitDepth = 16 // Required for Gemini Live API

	// Default timing constants - can be overridden via scenario.duplex.resilience config
	defaultInterTurnDelayMS         = 500
	defaultSelfplayInterTurnDelayMS = 1000
	defaultRetryDelayMS             = 1000
	defaultMaxRetries               = 0
	defaultPartialSuccessMinTurns   = 1
	defaultIgnoreLastTurnSessionEnd = true
	drainTimeoutSec                 = 30

	// Role constants
	roleAssistant = "assistant"
)

// errPartialSuccess is returned when a duplex conversation ends early but enough
// turns have completed to consider it a partial success. This is not a failure.
var errPartialSuccess = errors.New("partial success")

// errSessionEnded is returned when the session has ended (not an error, just complete)
var errSessionEnded = errors.New("session ended")

// responseAction indicates how a response element should be handled.
type responseAction int

const (
	// responseActionContinue means the element was informational (interruption signal),
	// and we should continue waiting for the final response.
	responseActionContinue responseAction = iota
	// responseActionComplete means we received a complete response.
	responseActionComplete
	// responseActionError means an error occurred or the response was empty.
	responseActionError
	// responseActionToolCalls means the response contains tool calls that need to be executed.
	responseActionToolCalls
)

// toolExecutionResult holds both the provider response and the messages for state store.
type toolExecutionResult struct {
	providerResponses []providers.ToolResponse
	resultMessages    []types.Message
}

// processResponseElement handles a response element from the pipeline, determining
// the appropriate action based on interruption signals, turn completion, and errors.
//
// This consolidates the response handling logic that was duplicated in
// streamAudioChunks and streamSelfPlayAudio.
//
// Returns:
//   - responseAction: what action to take
//   - error: any error to return (only set when action is responseActionError)
//
//nolint:gocognit // Complexity 17 is acceptable for response state handling
func processResponseElement(elem *stage.StreamElement, logPrefix string) (responseAction, error) {
	// Check for errors
	if elem.Error != nil {
		return responseActionError, elem.Error
	}

	// Check for interruption signals - these are informational, keep waiting
	if elem.Metadata != nil {
		// Interruption signal: Gemini detected user started speaking during response.
		// The partial response has been captured, now waiting for the new response.
		if interrupted, ok := elem.Metadata["interrupted"].(bool); ok && interrupted {
			logger.Debug(logPrefix + ": response interrupted, waiting for new response")
			return responseActionContinue, nil
		}

		// Interrupted turn complete: Empty turnComplete after interruption.
		// This is just Gemini closing the interrupted turn, not the final response.
		if itc, ok := elem.Metadata["interrupted_turn_complete"].(bool); ok && itc {
			logger.Debug(logPrefix + ": interrupted turn complete, waiting for real response")
			return responseActionContinue, nil
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
			return responseActionError, errors.New("empty response from Gemini, likely interrupted")
		}

		// If response contains tool calls, signal that tools need to be executed
		// The caller will execute tools and send results back, then continue waiting
		if hasToolCalls {
			logger.Debug(logPrefix+": received tool call response, needs execution",
				"tool_count", len(elem.Message.ToolCalls))
			return responseActionToolCalls, nil
		}

		return responseActionComplete, nil
	}

	// Element doesn't require action (e.g., streaming text/audio chunk)
	return responseActionContinue, nil
}
