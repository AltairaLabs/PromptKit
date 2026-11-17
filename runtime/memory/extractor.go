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

// ContextRequestExtractor is an Extractor that is given the whole request,
// caller included. The extraction stage calls ExtractRequest in place of
// Extract when the extractor implements it.
type ContextRequestExtractor interface {
	Extractor
	ExtractRequest(ctx context.Context, req ContextRequest) ([]*Memory, error)
}
