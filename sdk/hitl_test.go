package sdk

import (
	"context"
	"testing"

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

func TestCheckPending(t *testing.T) {
	t.Run("returns nil for non-async tool", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("normal_tool", func(args map[string]any) (any, error) {
			return "result", nil
		})

		pending, shouldWait := conv.CheckPending("normal_tool", map[string]any{})
		assert.Nil(t, pending)
		assert.False(t, shouldWait)
	})

	t.Run("returns nil when check passes", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnToolAsync(
			"conditional_tool",
			func(args map[string]any) sdktools.PendingResult {
				// Low value - no pending needed
				if args["amount"].(float64) < 100 {
					return sdktools.PendingResult{}
				}
				return sdktools.PendingResult{Reason: "high_value"}
			},
			func(args map[string]any) (any, error) {
				return "executed", nil
			},
		)

		pending, shouldWait := conv.CheckPending(
			"conditional_tool",
			map[string]any{"amount": float64(50)},
		)
		assert.Nil(t, pending)
		assert.False(t, shouldWait)
	})

	t.Run("creates pending when check requires approval", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnToolAsync(
			"risky_tool",
			func(args map[string]any) sdktools.PendingResult {
				return sdktools.PendingResult{
					Reason:  "always_pending",
					Message: "This tool always requires approval",
				}
			},
			func(args map[string]any) (any, error) {
				return "executed", nil
			},
		)

		pending, shouldWait := conv.CheckPending(
			"risky_tool",
			map[string]any{"key": "value"},
		)
		require.NotNil(t, pending)
		assert.True(t, shouldWait)
		assert.Equal(t, "risky_tool", pending.Name)
		assert.Equal(t, "always_pending", pending.Reason)
		assert.Equal(t, "This tool always requires approval", pending.Message)
		assert.NotEmpty(t, pending.ID)

		// Verify it's stored in pending store
		stored, ok := conv.pendingStore.Get(pending.ID)
		assert.True(t, ok)
		assert.Equal(t, pending.ID, stored.ID)
	})
}

func TestResolveTool(t *testing.T) {
	t.Run("returns error when no pending store", func(t *testing.T) {
		conv := newTestConversation()
		// Don't initialize pending store
		conv.pendingStore = nil

		_, err := conv.ResolveTool("some-id")
		assert.Error(t, err)
	})

	t.Run("returns error for non-existent id", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewPendingStore()

		_, err := conv.ResolveTool("non-existent")
		assert.Error(t, err)
	})
}

func TestRejectTool(t *testing.T) {
	t.Run("returns error when no pending store", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = nil

		_, err := conv.RejectTool("some-id", "reason")
		assert.Error(t, err)
	})

	t.Run("rejects pending tool", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewPendingStore()
		conv.pendingStore.Add(&sdktools.PendingToolCall{
			ID:   "test-id",
			Name: "test_tool",
		})

		resolution, err := conv.RejectTool("test-id", "not authorized")
		require.NoError(t, err)
		assert.True(t, resolution.Rejected)
		assert.Equal(t, "not authorized", resolution.RejectionReason)

		// Verify it's removed from store
		_, ok := conv.pendingStore.Get("test-id")
		assert.False(t, ok)
	})
}

func TestPendingTools(t *testing.T) {
	t.Run("returns nil when no store", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = nil

		pending := conv.PendingTools()
		assert.Nil(t, pending)
	})

	t.Run("returns pending tools", func(t *testing.T) {
		conv := newTestConversation()
		conv.pendingStore = sdktools.NewPendingStore()
		conv.pendingStore.Add(&sdktools.PendingToolCall{ID: "1", Name: "tool1"})
		conv.pendingStore.Add(&sdktools.PendingToolCall{ID: "2", Name: "tool2"})

		pending := conv.PendingTools()
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

	t.Run("returns error when no messages", func(t *testing.T) {
		conv := newTestConversation()
		// No messages in store

		_, err := conv.Continue(context.Background())
		assert.Error(t, err)
	})
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
	conv.CheckPending("async_tool", map[string]any{})

	// Fork
	forked := conv.Fork()

	// Verify async handlers are copied
	forked.asyncHandlersMu.RLock()
	_, hasHandler := forked.asyncHandlers["async_tool"]
	forked.asyncHandlersMu.RUnlock()
	assert.True(t, hasHandler)

	// Verify fork has fresh pending store (no pending calls)
	assert.NotNil(t, forked.pendingStore)
	assert.Equal(t, 0, forked.pendingStore.Len())

	// Original still has the pending call
	assert.Equal(t, 1, conv.pendingStore.Len())
}
