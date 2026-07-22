package sdk

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnToolAsync(t *testing.T) {
	conv := newTestConversation()

	t.Run("registers async handler", func(t *testing.T) {
		conv.OnToolAsync(
			"process_refund",
			func(args map[string]any) sdktools.PendingResult {
				amount := args["amount"].(float64)
				if amount > 100 {
					return sdktools.PendingResult{
						Reason:  "high_value",
						Message: "Amount exceeds $100",
					}
				}
				return sdktools.PendingResult{}
			},
			func(args map[string]any) (any, error) {
				return map[string]any{"status": "processed"}, nil
			},
		)

		// Verify async handler is registered
		conv.asyncHandlersMu.RLock()
		_, hasAsync := conv.asyncHandlers["process_refund"]
		conv.asyncHandlersMu.RUnlock()
		assert.True(t, hasAsync)

		// Verify execution handler is also registered
		conv.handlersMu.RLock()
		_, hasExec := conv.handlers["process_refund"]
		conv.handlersMu.RUnlock()
		assert.True(t, hasExec)
	})
}

// addPending stores a pending call scoped to the conversation and registers its
// execution handler by name, mirroring how the approval gate persists a held
// call plus how the handler is recovered by name at resolve time.
func addPending(t *testing.T, conv *Conversation, id, name string, args map[string]any, handler sdktools.ExecFunc) {
	t.Helper()
	if handler != nil {
		conv.handlersMu.Lock()
		conv.handlers[name] = ToolHandler(handler)
		conv.handlersMu.Unlock()
	}
	require.NoError(t, conv.pendingStore.Add(context.Background(), &sdktools.PendingToolCall{
		ID:             id,
		ConversationID: conv.ID(),
		Name:           name,
		Arguments:      args,
	}))
}

func TestResolveTool(t *testing.T) {
	t.Run("returns error when no pending store", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = nil

		_, err := conv.ResolveTool(context.Background(), "some-id")
		assert.Error(t, err)
	})

	t.Run("returns already-resolved for non-existent id", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()

		_, err := conv.ResolveTool(context.Background(), "non-existent")
		assert.ErrorIs(t, err, sdktools.ErrPendingAlreadyResolved)
	})
}

func TestResolveToolWithArgs(t *testing.T) {
	t.Run("returns error when no pending store", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = nil

		_, err := conv.ResolveToolWithArgs(context.Background(), "some-id", map[string]any{"x": 1})
		assert.Error(t, err)
	})

	t.Run("approves with edited args and records resolution", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
		conv.resolvedStore = sdktools.NewResolvedStore()

		var gotArgs map[string]any
		addPending(t, conv, "test-id", "send_message",
			map[string]any{"to": "Dana", "body": "original"},
			func(args map[string]any) (any, error) {
				gotArgs = args
				return map[string]any{"sent": args["body"]}, nil
			})

		resolution, err := conv.ResolveToolWithArgs(context.Background(), "test-id", map[string]any{"body": "edited"})
		require.NoError(t, err)
		assert.Equal(t, "edited", gotArgs["body"])
		assert.Equal(t, "Dana", gotArgs["to"]) // untouched original preserved
		assert.True(t, resolution.Edited)
		// The resolution is recorded for Continue()/ContinueDuplex() to consume.
		assert.Equal(t, 1, conv.resolvedStore.Len())
	})

	t.Run("ResolveTool delegates with no edits", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
		conv.resolvedStore = sdktools.NewResolvedStore()

		addPending(t, conv, "test-id", "t", map[string]any{"body": "original"},
			func(args map[string]any) (any, error) { return args["body"], nil })

		resolution, err := conv.ResolveTool(context.Background(), "test-id")
		require.NoError(t, err)
		assert.False(t, resolution.Edited)
	})

	t.Run("missing handler errors but preserves the held call for retry", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
		conv.resolvedStore = sdktools.NewResolvedStore()

		// Persist a held call but do NOT register a handler for it yet.
		addPending(t, conv, "test-id", "later_tool", map[string]any{"x": 1}, nil)

		_, err := conv.ResolveTool(context.Background(), "test-id")
		require.ErrorContains(t, err, "no handler registered")

		// The record must NOT have been claimed/destroyed — it is still there.
		still, err := conv.PendingTools(context.Background())
		require.NoError(t, err)
		require.Len(t, still, 1, "a missing handler must not consume the held call")

		// Registering the handler and retrying now succeeds.
		var ran bool
		conv.handlersMu.Lock()
		conv.handlers["later_tool"] = func(map[string]any) (any, error) { ran = true; return "ok", nil }
		conv.handlersMu.Unlock()

		_, err = conv.ResolveTool(context.Background(), "test-id")
		require.NoError(t, err)
		assert.True(t, ran, "retry after registering the handler must execute it")
	})
}

func TestRejectTool(t *testing.T) {
	t.Run("returns error when no pending store", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = nil

		_, err := conv.RejectTool(context.Background(), "some-id", "reason")
		assert.Error(t, err)
	})

	t.Run("rejects pending tool", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
		conv.resolvedStore = sdktools.NewResolvedStore()
		addPending(t, conv, "test-id", "test_tool", nil, nil)

		resolution, err := conv.RejectTool(context.Background(), "test-id", "not authorized")
		require.NoError(t, err)
		assert.True(t, resolution.Rejected)
		assert.Equal(t, "not authorized", resolution.RejectionReason)

		// Verify it's removed from store.
		_, ok, err := conv.pendingStore.Get(context.Background(), conv.ID(), "test-id")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestPendingStoreOwnership(t *testing.T) {
	t.Run("default store is owned by the SDK", func(t *testing.T) {
		store, owned := newPendingStore(&config{})
		require.NotNil(t, store)
		assert.True(t, owned, "an SDK-created store is closed on Conversation.Close")
	})

	t.Run("injected store is caller-owned and returned as-is", func(t *testing.T) {
		injected := sdktools.NewMemoryPendingStore()
		defer func() { _ = injected.Close() }()

		store, owned := newPendingStore(&config{pendingStore: injected})
		assert.Same(t, injected, store)
		assert.False(t, owned, "an injected store must not be closed by the SDK")
	})
}

func TestPendingTools(t *testing.T) {
	t.Run("returns nil when no store", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = nil

		pending, err := conv.PendingTools(context.Background())
		require.NoError(t, err)
		assert.Nil(t, pending)
	})

	t.Run("returns pending tools", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
		addPending(t, conv, "1", "tool1", nil, nil)
		addPending(t, conv, "2", "tool2", nil, nil)

		pending, err := conv.PendingTools(context.Background())
		require.NoError(t, err)
		assert.Len(t, pending, 2)
	})
}

func TestContinue(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.closed = true

		_, err := conv.Continue(context.Background())
		assert.Equal(t, ErrConversationClosed, err)
	})

	t.Run("returns error when no resolved tools", func(t *testing.T) {
		conv := newTestConversation()
		conv.resolvedStore = sdktools.NewResolvedStore()

		_, err := conv.Continue(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no resolved tools")
	})

	t.Run("returns error when nil resolved store", func(t *testing.T) {
		conv := newTestConversation()
		conv.resolvedStore = nil

		_, err := conv.Continue(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no resolved tools")
	})
}

func TestContinue_ResolutionBranches(t *testing.T) {
	// These tests exercise the resolution-building branches in Continue().
	// The minimal test pipeline processes messages without a provider, so
	// Continue succeeds — we verify the code paths don't panic.

	t.Run("builds rejected resolution message", func(t *testing.T) {
		conv := newTestConversation()
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:              "r1",
			Rejected:        true,
			RejectionReason: "not authorized",
		})

		resp, err := conv.Continue(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("builds error resolution message", func(t *testing.T) {
		conv := newTestConversation()
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:    "r2",
			Error: assert.AnError,
		})

		resp, err := conv.Continue(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("builds resultJSON resolution message", func(t *testing.T) {
		conv := newTestConversation()
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:         "r3",
			ResultJSON: []byte(`{"status":"ok"}`),
		})

		resp, err := conv.Continue(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("builds plain result resolution message", func(t *testing.T) {
		conv := newTestConversation()
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:     "r4",
			Result: "plain value",
		})

		resp, err := conv.Continue(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestResolveToolStoresResolution(t *testing.T) {
	t.Run("stores resolution for continue", func(t *testing.T) {
		conv := newTestConversation()

		// Register async tool with handler
		conv.OnToolAsync(
			"test_tool",
			func(args map[string]any) sdktools.PendingResult {
				return sdktools.PendingResult{Reason: "test"}
			},
			func(args map[string]any) (any, error) {
				return map[string]any{"status": "executed"}, nil
			},
		)

		// Persist a held call for that tool (as the approval gate would).
		addPending(t, conv, "call-1", "test_tool", map[string]any{"key": "value"}, nil)

		// Resolve it — the handler is recovered by name from OnToolAsync.
		resolution, err := conv.ResolveTool(context.Background(), "call-1")
		require.NoError(t, err)

		// Verify resolution was stored
		resolutions := conv.resolvedStore.PopAll()
		assert.Len(t, resolutions, 1)
		assert.Equal(t, resolution.ID, resolutions[0].ID)
	})
}

func TestRejectToolStoresResolution(t *testing.T) {
	t.Run("stores rejection for continue", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewMemoryPendingStore()
		defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
		conv.resolvedStore = sdktools.NewResolvedStore()

		addPending(t, conv, "test-id", "test_tool", nil, nil)

		// Reject it
		resolution, err := conv.RejectTool(context.Background(), "test-id", "not allowed")
		require.NoError(t, err)
		assert.True(t, resolution.Rejected)

		// Verify rejection was stored
		resolutions := conv.resolvedStore.PopAll()
		assert.Len(t, resolutions, 1)
		assert.True(t, resolutions[0].Rejected)
		assert.Equal(t, "not allowed", resolutions[0].RejectionReason)
	})
}

func TestContinueDuplex(t *testing.T) {
	t.Run("returns error in unary mode", func(t *testing.T) {
		conv := newTestConversation() // unary by default
		err := conv.ContinueDuplex(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplex mode")
	})

	t.Run("returns error when no resolved tools", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode
		conv.resolvedStore = sdktools.NewResolvedStore()

		err := conv.ContinueDuplex(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no resolved tools")
	})

	t.Run("builds rejected response", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:              "r1",
			Rejected:        true,
			RejectionReason: "not authorized",
		})
		conv.duplexSession = &fakeDuplexSession{}

		err := conv.ContinueDuplex(context.Background())
		require.NoError(t, err)
	})

	t.Run("builds error response", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:    "r2",
			Error: assert.AnError,
		})
		conv.duplexSession = &fakeDuplexSession{}

		err := conv.ContinueDuplex(context.Background())
		require.NoError(t, err)
	})

	t.Run("builds resultJSON response", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:         "r3",
			ResultJSON: []byte(`{"status":"ok"}`),
		})
		conv.duplexSession = &fakeDuplexSession{}

		err := conv.ContinueDuplex(context.Background())
		require.NoError(t, err)
	})

	t.Run("builds plain result response", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode
		conv.resolvedStore = sdktools.NewResolvedStore()
		conv.resolvedStore.Add(&sdktools.ToolResolution{
			ID:     "r4",
			Result: "plain value",
		})
		conv.duplexSession = &fakeDuplexSession{}

		err := conv.ContinueDuplex(context.Background())
		require.NoError(t, err)
	})
}

// fakeDuplexSession is a minimal DuplexSession implementation for testing ContinueDuplex.
type fakeDuplexSession struct{}

func (f *fakeDuplexSession) ID() string                     { return "fake" }
func (f *fakeDuplexSession) Variables() map[string]string   { return nil }
func (f *fakeDuplexSession) SetVar(_, _ string)             {}
func (f *fakeDuplexSession) GetVar(_ string) (string, bool) { return "", false }
func (f *fakeDuplexSession) Messages(_ context.Context) ([]types.Message, error) {
	return nil, nil
}
func (f *fakeDuplexSession) Clear(_ context.Context) error { return nil }
func (f *fakeDuplexSession) SendChunk(_ context.Context, _ *providers.StreamChunk) error {
	return nil
}
func (f *fakeDuplexSession) SendText(_ context.Context, _ string) error { return nil }
func (f *fakeDuplexSession) SendFrame(_ context.Context, _ *session.ImageFrame) error {
	return nil
}
func (f *fakeDuplexSession) SendVideoChunk(_ context.Context, _ *session.VideoChunk) error {
	return nil
}
func (f *fakeDuplexSession) Response() <-chan providers.StreamChunk { return nil }
func (f *fakeDuplexSession) Close() error                           { return nil }
func (f *fakeDuplexSession) Drain(_ context.Context) error          { return nil }
func (f *fakeDuplexSession) Done() <-chan struct{}                  { return nil }
func (f *fakeDuplexSession) Error() error                           { return nil }
func (f *fakeDuplexSession) SubmitToolResults(_ context.Context, _ []providers.ToolResponse) error {
	return nil
}
func (f *fakeDuplexSession) ForkSession(
	_ context.Context, _ string, _ session.PipelineBuilder,
) (session.DuplexSession, error) {
	return nil, nil
}

func TestForkWithAsyncHandlers(t *testing.T) {
	conv := newTestConversation()

	// Register async handler
	conv.OnToolAsync(
		"async_tool",
		func(args map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "test"}
		},
		func(args map[string]any) (any, error) {
			return "result", nil
		},
	)

	// Add a pending call
	addPending(t, conv, "call-1", "async_tool", map[string]any{}, nil)

	// Fork
	forked, err := conv.Fork()
	require.NoError(t, err)

	// Verify async handlers are copied
	forked.asyncHandlersMu.RLock()
	_, hasHandler := forked.asyncHandlers["async_tool"]
	forked.asyncHandlersMu.RUnlock()
	assert.True(t, hasHandler)

	// Verify fork has a fresh pending store (no pending calls)
	require.NotNil(t, forked.pendingStore)
	forkedPending, err := forked.PendingTools(context.Background())
	require.NoError(t, err)
	assert.Empty(t, forkedPending)

	// Original still has the pending call
	origPending, err := conv.PendingTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, origPending, 1)
}
