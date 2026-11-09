package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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
	d.turns.Store(5)

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

// failingSessionHook fails every lifecycle event.
type failingSessionHook struct{ name string }

func (h failingSessionHook) Name() string { return h.name }
func (h failingSessionHook) OnSessionStart(context.Context, hooks.SessionEvent) error {
	return errors.New("start broke")
}
func (h failingSessionHook) OnSessionUpdate(context.Context, hooks.SessionEvent) error {
	return errors.New("update broke")
}
func (h failingSessionHook) OnSessionEnd(context.Context, hooks.SessionEvent) error {
	return errors.New("end broke")
}

// The dispatcher discards session-hook errors so they never block the
// conversation; each must still leave a trace naming the hook, and must not
// stop the hooks registered after it (#2334).
func TestSessionHookDispatcher_FailingHookIsLoggedAndOthersStillRun(t *testing.T) {
	logs := captureLogs(t)
	after := &recordingSessionHook{name: "after"}
	reg := hooks.NewRegistry(
		hooks.WithSessionHook(failingSessionHook{name: "audit-sink"}),
		hooks.WithSessionHook(after),
	)
	d := newSessionHookDispatcher(reg, func() (string, string, []types.Message) {
		return "sess-9", "conv-9", nil
	})

	d.SessionStart(context.Background())
	d.SessionUpdate(context.Background())
	d.SessionEnd(context.Background())

	assert.True(t, after.startCalled && after.updateCalled && after.endCalled,
		"hooks after the failing one must still run")
	out := logs.String()
	for _, want := range []string{"start broke", "update broke", "end broke"} {
		assert.Contains(t, out, want)
	}
	assert.Equal(t, 3, strings.Count(out, "hook=audit-sink"), out)
	assert.Contains(t, out, "session_id=sess-9")
}
