package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Extractor derives memories from conversation messages.
// PromptKit defines the interface only. Implementations are provided
// by platform layers (Omnia) or SDK examples.
type Extractor interface {
	Extract(ctx context.Context, scope map[string]string, messages []types.Message) ([]*Memory, error)
}
