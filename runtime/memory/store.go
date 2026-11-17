package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Store is the core memory persistence interface. Every method other than Save
// operates within the scope it is given: a memory is visible only to calls
// that pass the same scope map it was saved with.
//
// The caller the conversation acts for (sdk.WithCaller) reaches a store on
// RetrieveOptions.Caller (memory__recall) and ListOptions.Caller
// (memory__list) whatever else it implements. Save and Delete take no
// options, so memory__remember and extracted memories reach it with the
// caller only through OptionsSaver, and memory__forget only through
// ExtrasDeleter (DeleteOptions.Caller). DeleteAll never receives it. A store
// that scopes reads by caller must implement OptionsSaver and ExtrasDeleter
// too, or a caller's writes are unauthorized; the SDK warns at startup about
// a store or retriever that takes the caller somewhere but lacks either.
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
// backend-specific arguments, or the caller (DeleteOptions.Caller), on
// delete. It is the only way memory__forget hands a store the caller.
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

// OptionsSaver is optionally implemented by stores that accept options on
// save, such as the caller the conversation acts for. It is the only way
// memory__remember and the extraction stage hand a store the caller. [Store.Save] takes no
// options, so a store opts in by implementing this; the memory executor and
// the extraction stage prefer it when present and fall back to Save
// otherwise.
type OptionsSaver interface {
	SaveWithOptions(ctx context.Context, memory *Memory, opts SaveOptions) error
}

// SaveFor saves m to store for caller: through SaveWithOptions when the store
// implements OptionsSaver, else through Save, which does not see the caller.
// The memory tools and the extraction stage save through it.
func SaveFor(ctx context.Context, store Store, m *Memory, caller types.Caller) error {
	if s, ok := store.(OptionsSaver); ok {
		return s.SaveWithOptions(ctx, m, SaveOptions{Caller: caller})
	}
	return store.Save(ctx, m)
}
