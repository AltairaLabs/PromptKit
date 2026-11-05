package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Store is the core memory persistence interface. Every method other than Save
// operates within the scope it is given: a memory is visible only to calls
// that pass the same scope map it was saved with.
type Store interface {
	// Save persists a memory under memory.Scope.
	Save(ctx context.Context, memory *Memory) error
	// Retrieve returns the memories in scope that match query, filtered by opts.
	// The tools call it for memory__recall.
	Retrieve(ctx context.Context, scope map[string]string, query string, opts RetrieveOptions) ([]*Memory, error)
	// List returns the memories in scope without a query, filtered and paged by
	// opts. The tools call it for memory__list.
	List(ctx context.Context, scope map[string]string, opts ListOptions) ([]*Memory, error)
	// Delete removes one memory by ID. The tools call it for memory__forget.
	Delete(ctx context.Context, scope map[string]string, memoryID string) error
	// DeleteAll removes every memory in scope.
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
// sdk.WithToolDescriptorOverride. Rather than change Delete's signature
// (which every Store implementation would have to follow), a store opts in
// by implementing this. The memory executor prefers it when present and
// falls back to Delete otherwise, so a store that ignores it is unaffected.
//
// Recall and list need no equivalent: RetrieveOptions and ListOptions are
// parameters, so Extras is a field on each of them.
type ExtrasDeleter interface {
	DeleteWithOptions(
		ctx context.Context, scope map[string]string, memoryID string, opts DeleteOptions,
	) error
}
