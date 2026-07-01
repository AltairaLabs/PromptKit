package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// captureSessionHook records every session lifecycle call for test assertions.
type captureSessionHook struct {
	mu      sync.Mutex
	starts  []hooks.SessionEvent
	updates []hooks.SessionEvent
	ends    []hooks.SessionEvent
}

func (h *captureSessionHook) Name() string { return "capture" }

func (h *captureSessionHook) OnSessionStart(_ context.Context, event hooks.SessionEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.starts = append(h.starts, event)
	return nil
}

func (h *captureSessionHook) OnSessionUpdate(_ context.Context, event hooks.SessionEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updates = append(h.updates, event)
	return nil
}

func (h *captureSessionHook) OnSessionEnd(_ context.Context, event hooks.SessionEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ends = append(h.ends, event)
	return nil
}

// makeMinimalConversationRequest returns a ConversationRequest wired with an
// in-memory state store and a 2-turn scripted scenario. The scripted executor
// appends one user + one assistant message per turn so the state store is
// populated for OnSessionUpdate event messages.
func makeMinimalConversationRequest(t *testing.T, turnCount int) (ConversationRequest, *MockTurnExecutor) {
	t.Helper()

	store := statestore.NewMemoryStore()

	scriptedExec := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "ok"},
			}
			if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
				if appender, ok := req.StateStoreConfig.Store.(statestore.MessageAppender); ok {
					return appender.AppendMessages(ctx, req.ConversationID, messages)
				}
			}
			return nil
		},
	}

	turns := make([]arenaconfig.TurnDefinition, turnCount)
	for i := range turns {
		turns[i] = arenaconfig.TurnDefinition{Role: "user", Content: "msg"}
	}

	req := ConversationRequest{
		Provider:       &MockProvider{id: "mock"},
		ConversationID: "test-conv",
		RunID:          "test-run",
		Scenario:       &arenaconfig.Scenario{ID: "sc", Turns: turns},
		Config: &arenaconfig.Config{
			Defaults: arenaconfig.Defaults{Temperature: 0, MaxTokens: 0},
		},
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	return req, scriptedExec
}

// TestSessionHook_FireOrder verifies that a 2-turn run fires:
//   - OnSessionStart exactly once (with non-nil tool_registry metadata)
//   - OnSessionUpdate once per turn with monotonically increasing TurnIndex
//   - OnSessionEnd exactly once, after all updates
func TestSessionHook_FireOrder(t *testing.T) {
	cap := &captureSessionHook{}
	reg := hooks.NewRegistry(hooks.WithSessionHook(cap))

	toolReg := tools.NewRegistry()

	req, scriptedExec := makeMinimalConversationRequest(t, 2)

	executor := NewDefaultConversationExecutor(
		scriptedExec,
		nil,
		nil,
		createTestPromptRegistry(t),
		nil,
	)

	// Thread the session hook registry into the context, simulating what
	// executeRun does before calling ExecuteConversation.
	sessionID := req.ConversationID
	meta := map[string]any{"tool_registry": toolReg}
	ctx := withSessionHookContext(context.Background(), reg, sessionID, meta)

	// Fire OnSessionStart manually (mirrors executeRun behaviour).
	require.NoError(t, reg.RunSessionStart(ctx, hooks.SessionEvent{
		SessionID:      sessionID,
		ConversationID: sessionID,
		Metadata:       meta,
	}))

	result := executor.ExecuteConversation(ctx, req)
	require.NotNil(t, result)
	require.False(t, result.Failed, "conversation should not fail: %s", result.Error)

	// Fire OnSessionEnd manually (mirrors executeRun behaviour).
	require.NoError(t, reg.RunSessionEnd(ctx, hooks.SessionEvent{
		SessionID:      sessionID,
		ConversationID: sessionID,
		Messages:       result.Messages,
		Metadata:       meta,
	}))

	// --- assertions ---

	// Start: exactly once
	assert.Len(t, cap.starts, 1, "OnSessionStart should fire exactly once")
	assert.Equal(t, sessionID, cap.starts[0].SessionID)
	assert.NotNil(t, cap.starts[0].Metadata["tool_registry"], "tool_registry must be non-nil on Start")

	// Update: once per turn, monotonically increasing TurnIndex
	assert.Len(t, cap.updates, 2, "OnSessionUpdate should fire once per turn")
	assert.Equal(t, 0, cap.updates[0].TurnIndex, "first update TurnIndex should be 0")
	assert.Equal(t, 1, cap.updates[1].TurnIndex, "second update TurnIndex should be 1")
	// Messages should grow — second update has more messages than first
	assert.Greater(t, len(cap.updates[1].Messages), len(cap.updates[0].Messages),
		"message count should grow with each turn update")

	// End: exactly once, after all updates
	assert.Len(t, cap.ends, 1, "OnSessionEnd should fire exactly once")
	assert.Equal(t, sessionID, cap.ends[0].SessionID)
	// End has the full conversation
	assert.NotEmpty(t, cap.ends[0].Messages, "OnSessionEnd should carry conversation messages")
}

// TestSessionHook_NoHooks verifies that running with no session hooks registered
// produces no panics and a normal result. This is the default no-op path.
func TestSessionHook_NoHooks(t *testing.T) {
	// nil registry is safe per hooks.Registry nil-safe contract
	var nilReg *hooks.Registry

	req, scriptedExec := makeMinimalConversationRequest(t, 2)

	executor := NewDefaultConversationExecutor(
		scriptedExec,
		nil,
		nil,
		createTestPromptRegistry(t),
		nil,
	)

	// No session hook context in the context — simulates Engine with nil sessionHooks.
	ctx := context.Background()

	require.NotPanics(t, func() {
		_ = nilReg.RunSessionStart(ctx, hooks.SessionEvent{SessionID: "x"})
	}, "nil registry must not panic on RunSessionStart")

	result := executor.ExecuteConversation(ctx, req)
	require.NotNil(t, result)
	require.False(t, result.Failed, "conversation should not fail without session hooks: %s", result.Error)

	require.NotPanics(t, func() {
		_ = nilReg.RunSessionEnd(ctx, hooks.SessionEvent{SessionID: "x", Messages: result.Messages})
	}, "nil registry must not panic on RunSessionEnd")
}

// TestSessionHookContext_RoundTrip verifies withSessionHookContext /
// sessionHookContextFrom correctly store and retrieve the registry, sessionID,
// and metadata.
func TestSessionHookContext_RoundTrip(t *testing.T) {
	reg := hooks.NewRegistry()
	meta := map[string]any{"k": "v"}

	ctx := withSessionHookContext(context.Background(), reg, "sid", meta)
	gotReg, gotID, gotMeta, ok := sessionHookContextFrom(ctx)

	require.True(t, ok)
	assert.Equal(t, reg, gotReg)
	assert.Equal(t, "sid", gotID)
	assert.Equal(t, "v", gotMeta["k"])
}

// TestSessionHookContext_Missing verifies that sessionHookContextFrom returns
// false when the context carries no session hook value.
func TestSessionHookContext_Missing(t *testing.T) {
	_, _, _, ok := sessionHookContextFrom(context.Background())
	assert.False(t, ok, "should return false for plain context")
}

// TestSessionHookContext_NilRegistrySkipsStore verifies that
// withSessionHookContext is a no-op when the registry is nil — the context is
// returned unchanged and sessionHookContextFrom subsequently returns false.
func TestSessionHookContext_NilRegistrySkipsStore(t *testing.T) {
	ctx := withSessionHookContext(context.Background(), nil, "sid", nil)
	_, _, _, ok := sessionHookContextFrom(ctx)
	assert.False(t, ok, "nil registry should not store anything in context")
}
