package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
