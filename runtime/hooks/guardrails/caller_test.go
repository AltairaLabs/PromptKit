package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerCapturingHandler reports the Caller each evaluation was handed.
type callerCapturingHandler struct {
	seen []types.Caller
}

func (h *callerCapturingHandler) Type() string { return "test_caller" }

func (h *callerCapturingHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	h.seen = append(h.seen, evalCtx.Caller)
	return &evals.EvalResult{Score: floatPtr(1.0)}, nil
}

// A guardrail's handler is given the caller the provider hook request
// carries, on both sides of the call.
func TestGuardrail_HandlerSeesTheRequestsCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}
	handler := &callerCapturingHandler{}
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(handler)
	hook, err := NewGuardrailHookFromRegistry("test_caller", map[string]any{"direction": "both"}, registry)
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Caller:   member,
	}
	require.True(t, hook.BeforeCall(context.Background(), req).Allow)
	require.True(t, hook.AfterCall(context.Background(), req,
		&hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "hello"}}).Allow)

	assert.Equal(t, []types.Caller{member, member}, handler.seen)
}

// streamingCallerHandler judges partial content with the caller, blocking
// one tenant.
type streamingCallerHandler struct {
	callerCapturingHandler
	partial []types.Caller
}

func (h *streamingCallerHandler) EvalPartialRequest(
	_ context.Context, req evals.PartialEvalRequest,
) (*evals.EvalResult, error) {
	h.partial = append(h.partial, req.Caller)
	if req.Caller.Claims["tenant"] == "blocked" {
		return &evals.EvalResult{Score: floatPtr(0)}, nil
	}
	return &evals.EvalResult{Score: floatPtr(1.0)}, nil
}

// A streaming guardrail judges each chunk with the caller the stage hands the
// chunk interceptors.
func TestGuardrail_StreamingHandlerSeesTheChunksCaller(t *testing.T) {
	handler := &streamingCallerHandler{}
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(handler)
	hook, err := NewGuardrailHookFromRegistry("test_caller", map[string]any{"direction": "output"}, registry)
	require.NoError(t, err)
	hooksReg := hooks.NewRegistry(hooks.WithProviderHook(hook))

	member := types.Caller{ID: "m", Claims: map[string]any{"tenant": "acme"}}
	blocked := types.Caller{ID: "b", Claims: map[string]any{"tenant": "blocked"}}
	allowed := hooksReg.RunOnChunkRequest(context.Background(),
		hooks.ChunkRequest{Chunk: &providers.StreamChunk{Content: "hello"}, Caller: member})
	denied := hooksReg.RunOnChunkRequest(context.Background(),
		hooks.ChunkRequest{Chunk: &providers.StreamChunk{Content: "hello"}, Caller: blocked})

	assert.True(t, allowed.Allow)
	assert.False(t, denied.Allow)
	assert.True(t, denied.Enforced)
	assert.Equal(t, []types.Caller{member, blocked}, handler.partial)
}
