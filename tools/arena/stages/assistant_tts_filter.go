package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// AssistantTTSFilterStage sits between the ArenaStateStoreSaveStage and the
// TTSStageWithInterruption in the VAD composed pipeline. It drops any Message
// element whose Role is not "assistant", preventing the user's own transcript
// (wrapped as a user Message by STTUserMessageStage) from being spoken back
// through TTS.
//
// All non-Message elements — audio, text, EndOfStream, and control elements —
// are forwarded unchanged so the TTS stage receives everything it needs.
//
// This is a Filter stage: it passes through assistant Messages and non-Message
// elements, and drops user/system/tool Messages.
type AssistantTTSFilterStage struct {
	stage.BaseStage
}

// NewAssistantTTSFilterStage creates a filter that only forwards assistant
// Message elements (and all non-Message elements) to the TTS stage.
func NewAssistantTTSFilterStage() *AssistantTTSFilterStage {
	return &AssistantTTSFilterStage{
		BaseStage: stage.NewBaseStage("assistant_tts_filter", stage.StageTypeTransform),
	}
}

// Process forwards every element unchanged except Message elements whose Role
// is not "assistant" — those are silently dropped.
func (s *AssistantTTSFilterStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if elem.Message != nil && elem.Message.Role != "assistant" {
			// Drop non-assistant messages (e.g. the user transcript) so TTS
			// never speaks the user's own words back at them.
			continue
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
