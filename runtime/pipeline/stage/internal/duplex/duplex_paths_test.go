package duplex

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These tests pin duplex paths that, before the move (#2876), only stage's
// tests reached, or reached only when a race went one way.

// pathBound bounds each call below that would block forever if the path it
// pins were broken.
const pathBound = 3 * time.Second

// RunsToolCalls is on exactly when SetToolExecution is given a registry.
func TestRunsToolCalls_FollowsTheRegistry(t *testing.T) {
	s := newDuplexStageForUnit()
	assert.False(t, s.RunsToolCalls(), "a new stage leaves tool calls to the host")
	s.SetToolExecution(tools.NewRegistry(), nil, DuplexToolConfig{}, nil)
	assert.True(t, s.RunsToolCalls())
	s.SetToolExecution(nil, nil, DuplexToolConfig{}, nil)
	assert.False(t, s.RunsToolCalls(), "a nil registry hands tool calls back to the host")
}

// Input that closes while the session is still being created ends the drain:
// what arrived before stays buffered, in order, for the session.
func TestBufferPreSessionInput_InputClosedWhileConnecting(t *testing.T) {
	a, b := "a", "b"
	input := make(chan core.StreamElement, 2)
	input <- core.StreamElement{Text: &a}
	input <- core.StreamElement{Text: &b}
	close(input)
	buffer := newPreSessionBuffer(defaultPreSessionBufferBytes)
	drainDone := make(chan struct{})
	go bufferPreSessionInput(context.Background(), input, buffer, &sync.Mutex{}, make(chan struct{}), drainDone)

	select {
	case <-drainDone:
	case <-time.After(pathBound):
		t.Fatal("the drain did not end when input closed")
	}
	var got []string
	for _, e := range buffer.elements() {
		got = append(got, *e.Text)
	}
	assert.Equal(t, []string{"a", "b"}, got)
}

// A typed user turn that cannot be queued before ctx ends is dropped with
// ctx's error.
func TestQueueTypedUserTurn_CanceledWhileTheQueueIsFull(t *testing.T) {
	s := newDuplexStageForUnit()
	s.blockedTurns = make(chan *duplexBlockedTurn) // nothing reads it: the queue is full
	s.pendingTypedUser = &types.Message{Role: core.RoleUser, Content: "hi"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	errs := make(chan error, 1)
	go func() { errs <- s.queueTypedUserTurn(ctx) }()
	select {
	case err := <-errs:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(pathBound):
		t.Fatal("queueTypedUserTurn did not give up when ctx ended")
	}
	assert.Nil(t, s.pendingTypedUser, "the turn is taken off the stage either way")
}

// Typed input whose user turn cannot be queued before ctx ends stops the
// input loop with ctx's error, and nothing is sent to the session.
func TestHandleInputElement_TypedTurnNotQueuedStopsForwarding(t *testing.T) {
	s := newDuplexStageForUnit()
	s.blockedTurns = make(chan *duplexBlockedTurn)
	sess := newRecordingSession()
	s.session = sess
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text := "hello"
	done := make(chan error, 1)

	stopped := make(chan bool, 1)
	go func() {
		stopped <- s.handleInputElement(ctx, &core.StreamElement{Text: &text}, make(chan core.StreamElement, 4), done)
	}()
	select {
	case stop := <-stopped:
		assert.True(t, stop, "forwarding stops")
	case <-time.After(pathBound):
		t.Fatal("handleInputElement did not return")
	}
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	default:
		t.Fatal("no result on done")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.texts, "the text is not sent to the session")
}

// Dropping the queue at session end skips a typed user turn and still
// reports the blocked turn behind it: its firing is recorded and its turn
// completed.
func TestDropBlockedTurns_SkipsATypedUserTurn(t *testing.T) {
	ts := core.NewTurnState()
	s := NewDuplexProviderStageWithTurnState(providersmock.NewStreamingProvider("t", "m", false), baseConfig(), nil, ts)
	blocked := &duplexBlockedTurn{msg: types.Message{Role: core.RoleAssistant, Content: "No."},
		decision: hooks.DenyWithMetadata("banned", map[string]any{"validator_type": "banned_words"})}
	s.blockedTurns <- &duplexBlockedTurn{user: &types.Message{Role: core.RoleUser, Content: "hi"}, userOnly: true}
	s.blockedTurns <- blocked

	s.dropBlockedTurns()

	assert.Empty(t, s.blockedTurns, "the queue is emptied")
	assert.Equal(t, 1, ts.SessionTurns(), "only the blocked turn completes a turn")
	require.Len(t, blocked.msg.Validations, 1, "the blocked turn's firing is recorded")
	v := blocked.msg.Validations[0]
	assert.Equal(t, "banned_words", v.ValidatorType)
	assert.False(t, v.Passed)
	assert.Equal(t, hooks.DirectionInput, v.Details["direction"])
}

// The cost of a turn goes on the last queued blocked reply, past any user-only
// turns after it, and on the partial reply when only user-only turns are
// queued.
func TestLastCostCarrier_SkipsUserOnlyTurns(t *testing.T) {
	partial := &types.Message{Role: core.RoleAssistant, Content: "partial"}
	user := &duplexBlockedTurn{user: &types.Message{Role: core.RoleUser, Content: "hi"}, userOnly: true}
	first := &duplexBlockedTurn{msg: types.Message{Role: core.RoleAssistant, Content: "first"}}
	second := &duplexBlockedTurn{msg: types.Message{Role: core.RoleAssistant, Content: "second"}}

	assert.Same(t, &second.msg, lastCostCarrier(partial, []*duplexBlockedTurn{first, second, user}))
	assert.Same(t, &first.msg, lastCostCarrier(partial, []*duplexBlockedTurn{first, user, user}))
	assert.Same(t, partial, lastCostCarrier(partial, []*duplexBlockedTurn{user, user}))
	assert.Nil(t, lastCostCarrier(nil, nil))
}
