package sdk

import sdka2a "github.com/AltairaLabs/PromptKit/sdk/internal/a2a"

// A2ATaskStore is the task persistence interface, re-exported from sdk/internal/a2a.
type A2ATaskStore = sdka2a.TaskStore

// InMemoryA2ATaskStore is a concurrency-safe in-memory TaskStore, re-exported from sdk/internal/a2a.
type InMemoryA2ATaskStore = sdka2a.InMemoryTaskStore

// Re-exported constructors and sentinel errors from sdk/internal/a2a.
var (
	NewInMemoryA2ATaskStore = sdka2a.NewInMemoryTaskStore

	ErrTaskNotFound      = sdka2a.ErrTaskNotFound
	ErrTaskAlreadyExists = sdka2a.ErrTaskAlreadyExists
	ErrInvalidTransition = sdka2a.ErrInvalidTransition
	ErrTaskTerminal      = sdka2a.ErrTaskTerminal
)
