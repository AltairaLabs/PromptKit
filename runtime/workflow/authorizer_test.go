package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubAuthorizer struct {
	err  error
	reqs []TransitionRequest
}

func (a *stubAuthorizer) AuthorizeTransition(_ context.Context, req TransitionRequest) error {
	a.reqs = append(a.reqs, req)
	return a.err
}

func authorizerSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "ta", OnEvent: map[string]string{"Go": "b", "Stay": "a"},
				Extensions: map[string]any{"acme:gate": "open"}},
			"b": {PromptTask: "tb"},
		},
	}
}

// TestCheckTransitionResolvesFromTheCurrentState — of state a's two events,
// the request names the one fired, with copies of both declarations.
func TestCheckTransitionResolvesFromTheCurrentState(t *testing.T) {
	spec := authorizerSpec()
	a := &stubAuthorizer{}

	require.NoError(t, CheckTransition(context.Background(), a, NewStateMachine(spec), spec, "Go"))

	require.Len(t, a.reqs, 1)
	req := a.reqs[0]
	require.Equal(t, "a", req.From)
	require.Equal(t, "b", req.To)
	require.Equal(t, "Go", req.Event)
	require.Equal(t, "ta", req.FromState.PromptTask)
	require.Equal(t, "tb", req.ToState.PromptTask)

	req.FromState.Extensions["acme:gate"] = "changed"
	require.Equal(t, "open", spec.States["a"].Extensions["acme:gate"])
}

// TestCheckTransitionSkipsWhatItCannotResolve — no authorizer, or an event
// the current state does not declare, never reaches the host.
func TestCheckTransitionSkipsWhatItCannotResolve(t *testing.T) {
	spec := authorizerSpec()
	a := &stubAuthorizer{err: errors.New("refuse everything")}

	require.NoError(t, CheckTransition(context.Background(), nil, NewStateMachine(spec), spec, "Go"))
	require.NoError(t, CheckTransition(context.Background(), a, NewStateMachine(spec), spec, "Unknown"))
	require.Empty(t, a.reqs)
}

// TestRefusedExecuteRecordsNothing — the executor returns a refusal result
// instead of scheduling, and leaves nothing pending.
func TestRefusedExecuteRecordsNothing(t *testing.T) {
	spec := authorizerSpec()
	exec := NewTransitionExecutor(NewStateMachine(spec), spec)
	exec.SetAuthorizer(&stubAuthorizer{err: errors.New("needs approval")})

	out, err := exec.Execute(context.Background(), nil, json.RawMessage(`{"event":"Go","context":"c"}`))

	require.NoError(t, err, "a refusal is a tool result for the model, not an error")
	var got map[string]string
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, map[string]string{
		"status": "transition_refused", "event": "Go", "reason": "needs approval",
	}, got)
	require.Nil(t, exec.Pending())
}

// TestAllowedExecuteSchedulesAsBefore — an allowing authorizer changes nothing
// about the deferred commit.
func TestAllowedExecuteSchedulesAsBefore(t *testing.T) {
	spec := authorizerSpec()
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)
	exec.SetAuthorizer(&stubAuthorizer{})

	_, err := exec.Execute(context.Background(), nil, json.RawMessage(`{"event":"Go","context":"c"}`))
	require.NoError(t, err)
	require.Equal(t, "Go", exec.Pending().Event)

	_, err = exec.CommitPending()
	require.NoError(t, err)
	require.Equal(t, "b", sm.CurrentState())
}
