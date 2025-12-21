package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
)

const (
	// Default duplex session timeout
	defaultDuplexTimeout = 10 * time.Minute

	// Default chunk size for audio streaming (20ms at 16kHz mono 16-bit)
	defaultAudioChunkSize = 640

	// Default sample rate for audio
	defaultSampleRate = 16000
)

// DuplexConversationExecutor handles duplex (bidirectional streaming) conversations.
// Unlike the standard executor which processes turns sequentially, this executor
// establishes a persistent streaming session and handles real-time audio I/O.
type DuplexConversationExecutor struct {
	selfPlayRegistry *selfplay.Registry
	promptRegistry   *prompt.Registry
	toolRegistry     *tools.Registry
	mediaStorage     storage.MediaStorageService
}

// NewDuplexConversationExecutor creates a new duplex conversation executor.
func NewDuplexConversationExecutor(
	selfPlayRegistry *selfplay.Registry,
	promptRegistry *prompt.Registry,
	toolRegistry *tools.Registry,
	mediaStorage storage.MediaStorageService,
) *DuplexConversationExecutor {
	return &DuplexConversationExecutor{
		selfPlayRegistry: selfPlayRegistry,
		promptRegistry:   promptRegistry,
		toolRegistry:     toolRegistry,
		mediaStorage:     mediaStorage,
	}
}

// ExecuteConversation runs a duplex conversation based on scenario.
// For duplex mode, this establishes a streaming session and processes
// audio turns in real-time.
func (de *DuplexConversationExecutor) ExecuteConversation(
	ctx context.Context,
	req ConversationRequest, //nolint:gocritic // Interface compliance requires value receiver
) *ConversationResult {
	// Validate duplex configuration
	if req.Scenario.Duplex == nil {
		return &ConversationResult{
			Failed: true,
			Error:  "duplex configuration required for DuplexConversationExecutor",
		}
	}

	if err := req.Scenario.Duplex.Validate(); err != nil {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("invalid duplex configuration: %v", err),
		}
	}

	// Create event emitter if event bus is configured
	var emitter *events.Emitter
	if req.EventBus != nil {
		emitter = events.NewEmitter(req.EventBus, req.RunID, "", req.ConversationID)
	}

	// Get timeout from config or use default
	timeout := req.Scenario.Duplex.GetTimeoutDuration(defaultDuplexTimeout)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Check if provider supports streaming input
	streamProvider, ok := req.Provider.(providers.StreamInputSupport)
	if !ok {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("provider %T does not support streaming input", req.Provider),
		}
	}

	// Execute duplex conversation
	result := de.executeDuplexConversation(ctx, &req, streamProvider, emitter)

	return result
}

// shouldUseClientVAD determines if client-side VAD should be used.
func (de *DuplexConversationExecutor) shouldUseClientVAD(req *ConversationRequest) bool {
	if req.Scenario.Duplex.TurnDetection == nil {
		return true // Default to VAD
	}
	return req.Scenario.Duplex.TurnDetection.Mode == config.TurnDetectionModeVAD
}

// buildVADConfig creates VAD configuration from scenario settings.
func (de *DuplexConversationExecutor) buildVADConfig(req *ConversationRequest) stage.AudioTurnConfig {
	cfg := stage.DefaultAudioTurnConfig()

	if req.Scenario.Duplex.TurnDetection != nil && req.Scenario.Duplex.TurnDetection.VAD != nil {
		vad := req.Scenario.Duplex.TurnDetection.VAD
		if vad.SilenceThresholdMs > 0 {
			cfg.SilenceDuration = time.Duration(vad.SilenceThresholdMs) * time.Millisecond
		}
		if vad.MinSpeechMs > 0 {
			cfg.MinSpeechDuration = time.Duration(vad.MinSpeechMs) * time.Millisecond
		}
		if vad.MaxTurnDurationS > 0 {
			cfg.MaxTurnDuration = time.Duration(vad.MaxTurnDurationS) * time.Second
		}
	}

	return cfg
}

// calculateTotalCost sums cost info from all messages.
func (de *DuplexConversationExecutor) calculateTotalCost(messages []types.Message) types.CostInfo {
	var total types.CostInfo
	for i := range messages {
		if messages[i].CostInfo != nil {
			total.InputTokens += messages[i].CostInfo.InputTokens
			total.OutputTokens += messages[i].CostInfo.OutputTokens
			total.TotalCost += messages[i].CostInfo.TotalCost
		}
	}
	return total
}

// containsSelfPlay checks if scenario uses self-play.
func (de *DuplexConversationExecutor) containsSelfPlay(scenario *config.Scenario) bool {
	for i := range scenario.Turns {
		if de.isSelfPlayRole(scenario.Turns[i].Role) {
			return true
		}
	}
	return false
}

// isSelfPlayRole checks if a role is configured for self-play.
func (de *DuplexConversationExecutor) isSelfPlayRole(role string) bool {
	if de.selfPlayRegistry == nil {
		return false
	}
	return de.selfPlayRegistry.IsValidRole(role)
}

// findFirstSelfPlayPersona finds the persona ID from the first self-play turn.
func (de *DuplexConversationExecutor) findFirstSelfPlayPersona(scenario *config.Scenario) string {
	for i := range scenario.Turns {
		if de.isSelfPlayRole(scenario.Turns[i].Role) && scenario.Turns[i].Persona != "" {
			return scenario.Turns[i].Persona
		}
	}
	return ""
}
