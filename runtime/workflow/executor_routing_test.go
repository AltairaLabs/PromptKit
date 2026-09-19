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

// Each conversation gets its own TransitionExecutor, registered into its own
// child registry (see tools.Registry.Child), so a transition is recorded
// against that conversation's state machine and no other. These pin the
// per-instance behavior the isolation relies on. See #2011.
func TestTransitionExecutor_RecordsAgainstItsOwnMachine(t *testing.T) {
	mine, _ := newRoutingExecutors(t)
	other, _ := newRoutingExecutors(t)

	_, err := mine.Execute(context.Background(), nil, json.RawMessage(`{"event":"Go","context":"because"}`))
	require.NoError(t, err)

	pending := mine.Pending()
	require.NotNil(t, pending)
	assert.Equal(t, "Go", pending.Event)
	assert.Equal(t, "because", pending.ContextSummary)

	assert.Nil(t, other.Pending(),
		"a second conversation's executor holds nothing")
}

func TestArtifactExecutor_WritesToItsOwnMachine(t *testing.T) {
	mineSM := NewStateMachine(routingSpec())
	otherSM := NewStateMachine(routingSpec())
	mine := NewArtifactExecutor(mineSM)
	_ = NewArtifactExecutor(otherSM)

	_, err := mine.Execute(context.Background(), nil, json.RawMessage(`{"name":"quote","value":"42"}`))
	require.NoError(t, err)

	assert.Equal(t, "42", mineSM.Context().Artifacts["quote"])
	assert.NotContains(t, otherSM.Context().Artifacts, "quote")
}

var (
	_ tools.Executor = (*TransitionExecutor)(nil)
	_ tools.Executor = (*ArtifactExecutor)(nil)
)
