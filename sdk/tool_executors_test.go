package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/require"
)

// TestLocalExecutor_ResolvesHandlersRegisteredAfterBuild covers the duplex gap:
// a duplex pipeline builds its localExecutor ONCE at OpenDuplex, before the app
// registers tool handlers (OnTool/OnToolCtx). With only the build-time snapshot
// the executor cannot see those handlers and fails with "no handler registered";
// the live accessor lets it read the conversation's handlers at call time.
func TestLocalExecutor_ResolvesHandlersRegisteredAfterBuild(t *testing.T) {
	conv := &Conversation{
		handlers:    map[string]ToolHandler{},
		ctxHandlers: map[string]ToolHandlerCtx{},
	}

	// Executor built with an EMPTY snapshot (as in a duplex pipeline built before
	// any handler is registered) but wired to the live accessor.
	exec := &localExecutor{
		handlers:    map[string]ToolHandler{},
		ctxHandlers: map[string]ToolHandlerCtx{},
		live:        &localHandlersAccessor{conv: conv},
	}

	// Register a ctx handler AFTER the executor was built (post-OpenDuplex).
	var called bool
	conv.handlersMu.Lock()
	conv.ctxHandlers["echo"] = func(_ context.Context, args map[string]any) (any, error) {
		called = true
		return map[string]any{"echoed": args["msg"]}, nil
	}
	conv.handlersMu.Unlock()

	out, err := exec.Execute(context.Background(),
		&tools.ToolDescriptor{Name: "echo"}, json.RawMessage(`{"msg":"hi"}`))
	require.NoError(t, err)
	require.True(t, called, "a handler registered after build must dispatch via the live accessor")
	require.JSONEq(t, `{"echoed":"hi"}`, string(out))
}

// TestLocalExecutor_PrefersPlainHandlerViaLiveAccessor confirms the plain
// (non-ctx) handler path is also resolved live.
func TestLocalExecutor_PrefersPlainHandlerViaLiveAccessor(t *testing.T) {
	conv := &Conversation{
		handlers:    map[string]ToolHandler{},
		ctxHandlers: map[string]ToolHandlerCtx{},
	}
	exec := &localExecutor{
		handlers:    map[string]ToolHandler{},
		ctxHandlers: map[string]ToolHandlerCtx{},
		live:        &localHandlersAccessor{conv: conv},
	}

	conv.handlersMu.Lock()
	conv.handlers["ping"] = func(map[string]any) (any, error) { return "pong", nil }
	conv.handlersMu.Unlock()

	out, err := exec.Execute(context.Background(),
		&tools.ToolDescriptor{Name: "ping"}, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `"pong"`, string(out))
}

// TestLocalExecutor_NoAccessorMissesLateHandlers pins the pre-fix behavior: with
// only a build-time snapshot and no live accessor, a handler registered later is
// invisible — this is exactly what broke duplex + local tools.
func TestLocalExecutor_NoAccessorMissesLateHandlers(t *testing.T) {
	exec := &localExecutor{
		handlers:    map[string]ToolHandler{},
		ctxHandlers: map[string]ToolHandlerCtx{},
		// no live accessor
	}
	_, err := exec.Execute(context.Background(),
		&tools.ToolDescriptor{Name: "echo"}, json.RawMessage(`{}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no handler registered")
}
