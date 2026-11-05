package hooks

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
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

// ToolResultRewriter is an opt-in extension for ToolHook. A ToolHook that
// also implements it may rewrite the text of a tool's successful result
// before the model sees it, as a test harness does to plant an indirect
// prompt injection in live tool output.
//
// The provider stage calls it for the tools it runs: once with a plain
// result's text, or once per text part of a multimodal result, before the
// result size limit applies. Rewriters run in registration order, each on
// the previous one's output; an error fails the tool call. Client tools,
// calls resumed after approval and duplex sessions are not rewritten.
// Parallel tool calls rewrite concurrently, so RewriteResult must be safe
// for concurrent use.
type ToolResultRewriter interface {
	RewriteResult(ctx context.Context, req ToolRequest, content string) (string, error)
}

// SessionHook tracks session lifecycle.
type SessionHook interface {
	Name() string
	OnSessionStart(ctx context.Context, event SessionEvent) error
	OnSessionUpdate(ctx context.Context, event SessionEvent) error
	OnSessionEnd(ctx context.Context, event SessionEvent) error
}
