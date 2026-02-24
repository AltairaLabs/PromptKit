package hooks

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// ProviderHook intercepts LLM provider calls.
type ProviderHook interface {
	Name() string
	BeforeCall(ctx context.Context, req *ProviderRequest) Decision
	AfterCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision
}

// ChunkInterceptor is an opt-in streaming extension for ProviderHook.
// ProviderHooks that also implement ChunkInterceptor will have OnChunk
// called for each streaming chunk, enabling early abort.
type ChunkInterceptor interface {
	OnChunk(ctx context.Context, chunk *providers.StreamChunk) Decision
}

// ToolHook intercepts tool execution (LLM-initiated calls only).
type ToolHook interface {
	Name() string
	BeforeExecution(ctx context.Context, req ToolRequest) Decision
	AfterExecution(ctx context.Context, req ToolRequest, resp ToolResponse) Decision
}

// SessionHook tracks session lifecycle.
type SessionHook interface {
	Name() string
	OnSessionStart(ctx context.Context, event SessionEvent) error
	OnSessionUpdate(ctx context.Context, event SessionEvent) error
	OnSessionEnd(ctx context.Context, event SessionEvent) error
}
