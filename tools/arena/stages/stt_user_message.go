package stages

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// STTUserMessageStage converts a transcribed text element (as produced by the
// STT stage) into a user Message element so the downstream prompt-assembly,
// provider, and state-store-save stages treat the spoken turn exactly like a
// typed user message. This is the bridge that makes the composed VAD voice
// pipeline materialize history identically to a text run: ProviderStage only
// accumulates Message elements, so a bare Text element from STT would otherwise
// never reach the provider or be persisted.
//
// Text-only elements are wrapped; elements that already carry a Message, plus
// audio / EndOfStream / non-text elements, are forwarded unchanged.
//
// This is a Transform stage: text element -> user message element (1:1).
type STTUserMessageStage struct {
	stage.BaseStage
}

// NewSTTUserMessageStage creates a stage that wraps STT text into user messages.
func NewSTTUserMessageStage() *STTUserMessageStage {
	return &STTUserMessageStage{
		BaseStage: stage.NewBaseStage("stt_user_message", stage.StageTypeTransform),
	}
}

// Process wraps text elements into user message elements and forwards the rest.
func (s *STTUserMessageStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	for elem := range input {
		out := s.wrap(elem)
		select {
		case output <- out:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// wrap converts a text-only element into a user message element, preserving the
// element metadata. All other elements pass through unchanged.
//
//nolint:gocritic // hugeParam: StreamElement is the stage element type, passed by value by contract.
func (s *STTUserMessageStage) wrap(elem stage.StreamElement) stage.StreamElement {
	if elem.Message != nil || elem.Text == nil {
		return elem
	}
	text := strings.TrimSpace(*elem.Text)
	if text == "" {
		return elem
	}
	msg := types.NewUserMessage(text)
	wrapped := stage.NewMessageElement(&msg)
	wrapped.Meta = elem.Meta
	return wrapped
}
