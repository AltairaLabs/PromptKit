package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// These tests run the workflow tools the way a conversation does: the
// descriptor registered for the current state, the executor registered
// beside it, and the model's call sent through tools.Registry.Execute, which
// coerces and validates the arguments against the descriptor before the
// executor sees them. The executors' own tests call Execute directly and so
// never see what the registry does to the arguments in between.

func seamSpec() *Spec {
	return &Spec{
		Version: 2,
		Entry:   "intake",
		States: map[string]*State{
			"intake": {
				PromptTask: "t",
				OnEvent:    map[string]string{"Escalate": "human", "Resolve": "done"},
				Artifacts:  map[string]*ArtifactDef{"order_id": {}},
			},
			"human": {PromptTask: "t", OnEvent: map[string]string{"Resolve": "done"}},
			"done":  {PromptTask: "t"},
		},
	}
}

func TestWorkflowTools_ThroughToolRegistry(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*tools.Registry, *TransitionExecutor, *StateMachine, *Spec) {
		t.Helper()
		spec := seamSpec()
		sm := NewStateMachine(spec)
		reg := tools.NewRegistry()
		exec := NewTransitionExecutor(sm, spec)
		reg.RegisterExecutor(exec)
		reg.RegisterExecutor(NewArtifactExecutor(sm))
		exec.RegisterForState(reg, spec.States[sm.CurrentState()])
		RegisterArtifactTool(reg, spec)
		return reg, exec, sm, spec
	}

	t.Run("a weak model's event is normalized before the executor records it", func(t *testing.T) {
		reg, exec, sm, _ := setup(t)
		res, err := reg.Execute(ctx, TransitionToolName,
			json.RawMessage(`{"event": "escalate ", "context": "angry customer"}`))
		require.NoError(t, err)
		require.Empty(t, res.Error)
		assert.JSONEq(t, `{"status":"transition_scheduled","event":"Escalate","target_state":"human"}`,
			string(res.Result))

		require.NotNil(t, exec.Pending())
		assert.Equal(t, "Escalate", exec.Pending().Event)
		assert.Equal(t, "angry customer", exec.Pending().ContextSummary)

		tr, err := exec.CommitPending()
		require.NoError(t, err)
		assert.Equal(t, "human", tr.To)
		assert.Equal(t, "human", sm.CurrentState())
	})

	t.Run("an event the state does not offer never reaches the executor", func(t *testing.T) {
		reg, exec, sm, _ := setup(t)
		_, err := reg.Execute(ctx, TransitionToolName,
			json.RawMessage(`{"event": "Cancel", "context": "x"}`))
		var verr *tools.ValidationError
		require.True(t, errors.As(err, &verr), "want *tools.ValidationError, got %v", err)
		assert.Equal(t, "args_invalid", verr.Type)
		assert.Contains(t, verr.Detail, "event must be one of the following")
		assert.Nil(t, exec.Pending())
		assert.Equal(t, "intake", sm.CurrentState())
	})

	t.Run("re-registering for the new state narrows the events the registry accepts", func(t *testing.T) {
		reg, exec, sm, spec := setup(t)
		_, err := reg.Execute(ctx, TransitionToolName, json.RawMessage(`{"event":"Escalate","context":"c"}`))
		require.NoError(t, err)
		_, err = exec.CommitPending()
		require.NoError(t, err)
		exec.RegisterForState(reg, spec.States[sm.CurrentState()])

		_, err = reg.Execute(ctx, TransitionToolName, json.RawMessage(`{"event":"Escalate","context":"c"}`))
		var verr *tools.ValidationError
		require.True(t, errors.As(err, &verr), "Escalate is not offered from human; got %v", err)
		assert.Nil(t, exec.Pending())

		res, err := reg.Execute(ctx, TransitionToolName, json.RawMessage(`{"event":"resolve","context":"c"}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"status":"transition_scheduled","event":"Resolve","target_state":"done"}`,
			string(res.Result))
		_, err = exec.CommitPending()
		require.NoError(t, err)

		exec.RegisterForState(reg, spec.States[sm.CurrentState()])
		_, err = reg.Execute(ctx, TransitionToolName, json.RawMessage(`{"event":"Resolve","context":"c"}`))
		require.ErrorIs(t, err, tools.ErrToolNotFound, "a terminal state offers no transition tool")
	})

	t.Run("a weak model's artifact name is normalized before it is stored", func(t *testing.T) {
		reg, _, sm, _ := setup(t)
		res, err := reg.Execute(ctx, ArtifactToolName, json.RawMessage(`{"name": "Order_ID", "value": "A-17"}`))
		require.NoError(t, err)
		require.Empty(t, res.Error)
		assert.JSONEq(t, `{"status":"artifact_set","artifact":"order_id"}`, string(res.Result))
		assert.Equal(t, map[string]string{"order_id": "A-17"}, sm.Context().Artifacts)

		_, err = reg.Execute(ctx, ArtifactToolName, json.RawMessage(`{"name": "invoice", "value": "x"}`))
		var verr *tools.ValidationError
		require.True(t, errors.As(err, &verr), "an undeclared artifact is refused; got %v", err)
		assert.Equal(t, map[string]string{"order_id": "A-17"}, sm.Context().Artifacts)
	})
}
