package sdk

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

// Type aliases for backwards compatibility — re-exported from server/a2a.

// A2ATaskStore is the task persistence interface.
type A2ATaskStore = a2aserver.TaskStore

// InMemoryA2ATaskStore is a concurrency-safe in-memory TaskStore.
type InMemoryA2ATaskStore = a2aserver.InMemoryTaskStore

// A2AServer is the A2A-protocol HTTP server.
type A2AServer = a2aserver.Server

// A2AServerOption configures an A2AServer.
type A2AServerOption = a2aserver.Option

// A2AConversationOpener creates or retrieves a conversation for a context ID.
type A2AConversationOpener = a2aserver.ConversationOpener

// Re-exported constructors and sentinel errors.
var (
	NewInMemoryA2ATaskStore = a2aserver.NewInMemoryTaskStore

	ErrTaskNotFound      = a2aserver.ErrTaskNotFound
	ErrTaskAlreadyExists = a2aserver.ErrTaskAlreadyExists
	ErrInvalidTransition = a2aserver.ErrInvalidTransition
	ErrTaskTerminal      = a2aserver.ErrTaskTerminal
)

// NewA2AServer creates a new A2A server with the given opener and options.
func NewA2AServer(opener A2AConversationOpener, opts ...A2AServerOption) *A2AServer {
	return a2aserver.NewServer(opener, opts...)
}

// Re-exported option functions with backwards-compatible names.

// WithA2ACard sets the agent card served at /.well-known/agent.json.
func WithA2ACard(card *a2a.AgentCard) A2AServerOption {
	return a2aserver.WithCard(card)
}

// WithA2APort sets the TCP port for ListenAndServe.
func WithA2APort(port int) A2AServerOption {
	return a2aserver.WithPort(port)
}

// WithA2ATaskStore sets a custom task store.
func WithA2ATaskStore(store A2ATaskStore) A2AServerOption {
	return a2aserver.WithTaskStore(store)
}

// WithA2AReadTimeout sets the read timeout.
func WithA2AReadTimeout(d time.Duration) A2AServerOption {
	return a2aserver.WithReadTimeout(d)
}

// WithA2AWriteTimeout sets the write timeout.
func WithA2AWriteTimeout(d time.Duration) A2AServerOption {
	return a2aserver.WithWriteTimeout(d)
}

// WithA2AIdleTimeout sets the idle timeout.
func WithA2AIdleTimeout(d time.Duration) A2AServerOption {
	return a2aserver.WithIdleTimeout(d)
}

// WithA2AMaxBodySize sets the max body size.
func WithA2AMaxBodySize(n int64) A2AServerOption {
	return a2aserver.WithMaxBodySize(n)
}

// WithA2ATaskTTL sets the task TTL for eviction.
func WithA2ATaskTTL(d time.Duration) A2AServerOption {
	return a2aserver.WithTaskTTL(d)
}

// WithA2AConversationTTL sets the conversation TTL for eviction.
func WithA2AConversationTTL(d time.Duration) A2AServerOption {
	return a2aserver.WithConversationTTL(d)
}
