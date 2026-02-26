package sdk

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestSessionHookDispatcher_NilReceiver(t *testing.T) {
	var d *sessionHookDispatcher

	// All methods should be safe on a nil receiver.
	d.SessionStart(context.Background())
	d.SessionUpdate(context.Background())
	d.SessionEnd(context.Background())
	d.IncrementTurn()
	assert.Equal(t, 0, d.TurnIndex())
}

func TestSessionHookDispatcher_NilRegistry(t *testing.T) {
	d := newSessionHookDispatcher(nil, func() (string, string, []types.Message) {
		return "s1", "c1", nil
	})

	// Should not panic — dispatch is a no-op.
	d.SessionStart(context.Background())
	d.SessionUpdate(context.Background())
	d.SessionEnd(context.Background())
}

func TestSessionHookDispatcher_DispatchesAllLifecycleEvents(t *testing.T) {
	hook := &recordingSessionHook{name: "lifecycle"}
	reg := hooks.NewRegistry(hooks.WithSessionHook(hook))

	d := newSessionHookDispatcher(reg, func() (string, string, []types.Message) {
		return "sess-1", "conv-1", []types.Message{{Role: "user"}}
	})

	d.SessionStart(context.Background())
	assert.True(t, hook.startCalled)
	assert.Equal(t, "sess-1", hook.lastEvent.SessionID)
	assert.Equal(t, "conv-1", hook.lastEvent.ConversationID)
	assert.Equal(t, 0, hook.lastEvent.TurnIndex)
	assert.Len(t, hook.lastEvent.Messages, 1)

	d.IncrementTurn()
	d.IncrementTurn()
	d.SessionUpdate(context.Background())
	assert.True(t, hook.updateCalled)
	assert.Equal(t, 2, hook.lastEvent.TurnIndex)

	d.SessionEnd(context.Background())
	assert.True(t, hook.endCalled)
}

func TestSessionHookDispatcher_BuildEventNilInfo(t *testing.T) {
	d := newSessionHookDispatcher(nil, nil)
	d.turns = 5

	event := d.buildEvent()

	assert.Equal(t, 5, event.TurnIndex)
	assert.Empty(t, event.SessionID)
	assert.Empty(t, event.ConversationID)
	assert.Nil(t, event.Messages)
}

func TestSessionHookDispatcher_TurnIndex(t *testing.T) {
	d := newSessionHookDispatcher(nil, nil)
	assert.Equal(t, 0, d.TurnIndex())

	d.IncrementTurn()
	assert.Equal(t, 1, d.TurnIndex())

	d.IncrementTurn()
	d.IncrementTurn()
	assert.Equal(t, 3, d.TurnIndex())
}
