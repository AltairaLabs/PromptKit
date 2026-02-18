package statestore

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Summarizer compresses a batch of messages into a summary string.
// Implementations may use LLM providers, extractive methods, or other
// compression strategies.
type Summarizer interface {
	// Summarize compresses the given messages into a concise summary.
	Summarize(ctx context.Context, messages []types.Message) (string, error)
}
