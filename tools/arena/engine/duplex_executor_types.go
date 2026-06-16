package engine

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/streaming"
)

const (
	// Audio configuration constants
	geminiAudioBitDepth = 16 // Required for Gemini Live API

	// Default timing constants - can be overridden via scenario.duplex.resilience config
	defaultRetryDelayMS             = 1000
	defaultMaxRetries               = 0
	defaultPartialSuccessMinTurns   = 1
	defaultIgnoreLastTurnSessionEnd = true
	drainTimeoutSec                 = 30

	// Role constants
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// errPartialSuccess is returned when a duplex conversation ends early but enough
// turns have completed to consider it a partial success. This is not a failure.
var errPartialSuccess = errors.New("partial success")

// errSessionEnded wraps the runtime streaming package's ErrSessionEnded for arena-specific handling.
var errSessionEnded = streaming.ErrSessionEnded
