package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

func routingSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*State{
			"start": {PromptTask: "t", OnEvent: map[string]string{"Go": "done"}},
			"done":  {PromptTask: "t"},
		},
	}
}

func newRoutingExecutors(t *testing.T) (*TransitionExecutor, *StateMachine) {
	t.Helper()
	spec := routingSpec()
	sm := NewStateMachine(spec)
	return NewTransitionExecutor(sm, spec), sm
}

// A conversation's workflow__transition must be recorded against its OWN
// state machine, even when another conversation's executor is the one the
// tools.Registry holds under "workflow-transition".
//
// A Registry keeps exactly one executor per name. Two workflow conversations
// sharing a registry meant the second one's executor received the first's
// transition: the first's CommitPending then found nothing pending and
// silently dropped the transition, while the second committed a transition its
// own model never asked for. See AltairaLabs/PromptKit#2011.
func TestTransitionExecutor_RoutesToTheContextTarget(t *testing.T) {
	mine, _ := newRoutingExecutors(t)
	registered, _ := newRoutingExecutors(t)

	ctx := WithTransitionExecutor(context.Background(), mine)
	_, err := registered.Execute(ctx, nil, json.RawMessage(`{"event":"Go","context":"because"}`))
	require.NoError(t, err)

	pending := mine.Pending()
	require.NotNil(t, pending, "the transition belongs to the caller's own executor")
	assert.Equal(t, "Go", pending.Event)
	assert.Equal(t, "because", pending.ContextSummary)

	assert.Nil(t, registered.Pending(),
		"the registered executor must not absorb another conversation's transition")
}

// With no target on the context the registered executor handles the call, so
// hosts that never adopt the context path are unaffected.
func TestTransitionExecutor_FallsBackToItself(t *testing.T) {
	registered, _ := newRoutingExecutors(t)

	_, err := registered.Execute(context.Background(), nil, json.RawMessage(`{"event":"Go"}`))
	require.NoError(t, err)

	require.NotNil(t, registered.Pending())
	assert.Equal(t, "Go", registered.Pending().Event)
}

// The same for artifacts, which mutate the state machine immediately.
func TestArtifactExecutor_RoutesToTheContextTarget(t *testing.T) {
	mineSM := NewStateMachine(routingSpec())
	registeredSM := NewStateMachine(routingSpec())
	mine := NewArtifactExecutor(mineSM)
	registered := NewArtifactExecutor(registeredSM)

	ctx := WithArtifactExecutor(context.Background(), mine)
	_, err := registered.Execute(ctx, nil, json.RawMessage(`{"name":"quote","value":"42"}`))
	require.NoError(t, err)

	assert.Equal(t, "42", mineSM.Context().Artifacts["quote"],
		"the artifact belongs to the caller's own state machine")
	assert.NotContains(t, registeredSM.Context().Artifacts, "quote",
		"another conversation's state machine must be untouched")
}

// And the artifact fallback.
func TestArtifactExecutor_FallsBackToItself(t *testing.T) {
	sm := NewStateMachine(routingSpec())
	registered := NewArtifactExecutor(sm)

	_, err := registered.Execute(context.Background(), nil, json.RawMessage(`{"name":"quote","value":"42"}`))
	require.NoError(t, err)

	assert.Equal(t, "42", sm.Context().Artifacts["quote"])
}

// Nil targets are ignored rather than redirecting the call into nothing.
func TestWorkflowExecutorContext_NilIsANoOp(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, ctx, WithTransitionExecutor(ctx, nil))
	assert.Equal(t, ctx, WithArtifactExecutor(ctx, nil))
}

var (
	_ tools.Executor = (*TransitionExecutor)(nil)
	_ tools.Executor = (*ArtifactExecutor)(nil)
)
