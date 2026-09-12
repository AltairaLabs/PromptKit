package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Store is the core memory persistence interface.
type Store interface {
	Save(ctx context.Context, memory *Memory) error
	Retrieve(ctx context.Context, scope map[string]string, query string, opts RetrieveOptions) ([]*Memory, error)
	List(ctx context.Context, scope map[string]string, opts ListOptions) ([]*Memory, error)
	Delete(ctx context.Context, scope map[string]string, memoryID string) error
	DeleteAll(ctx context.Context, scope map[string]string) error
}

// ToolProvider is optionally implemented by stores that want to register
// additional tools beyond the base recall/remember/list/forget.
// Custom tools are registered in the "memory" namespace.
type ToolProvider interface {
	RegisterTools(registry *tools.Registry)
}

// ExtrasDeleter is optionally implemented by stores that accept
// backend-specific arguments on delete.
//
// [Store.Delete] takes no options struct, so it has nowhere to carry the
// passthrough args a host adds to memory__forget's input schema with
// sdk.WithToolDescriptorOverride. Rather than change Delete's signature —
// which every Store implementation would have to follow — a store opts in
// by implementing this. The memory executor prefers it when present and
// falls back to Delete otherwise, so a store that ignores it is unaffected.
//
// Recall and list need no equivalent: RetrieveOptions and ListOptions were
// already parameters, so Extras went straight onto them.
// See AltairaLabs/PromptKit#1987.
type ExtrasDeleter interface {
	DeleteWithOptions(
		ctx context.Context, scope map[string]string, memoryID string, opts DeleteOptions,
	) error
}
