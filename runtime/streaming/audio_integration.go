package streaming

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// SendEndOfStream signals that audio input is complete for the current turn.
// This triggers the provider to generate a response.
func SendEndOfStream(
	ctx context.Context,
	inputChan chan<- stage.StreamElement,
) error {
	logger.Debug("Sending EndOfStream signal to trigger response")
	endOfTurn := stage.StreamElement{EndOfStream: true}
	select {
	case inputChan <- endOfTurn:
		logger.Debug("EndOfStream signal sent")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
