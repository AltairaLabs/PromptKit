package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Extractor derives memories from conversation messages.
// PromptKit defines the interface only. Implementations come from the host
// application or a platform layer.
type Extractor interface {
	Extract(ctx context.Context, scope map[string]string, messages []types.Message) ([]*Memory, error)
}
