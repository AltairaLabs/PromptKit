package stages

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// StripToolMessagesStage removes tool role messages from the stream.
// This is used in self-play scenarios before calling the self-play provider.
type StripToolMessagesStage struct {
	stage.BaseStage
}

// NewStripToolMessagesStage creates a new strip tool messages stage.
func NewStripToolMessagesStage() *StripToolMessagesStage {
	return &StripToolMessagesStage{
		BaseStage: stage.NewBaseStage("strip_tool_messages", stage.StageTypeTransform),
	}
}

// Process filters out elements with tool role messages.
func (s *StripToolMessagesStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Skip elements with tool role messages
		if elem.Message != nil && strings.EqualFold(elem.Message.Role, "tool") {
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
