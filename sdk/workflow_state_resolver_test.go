package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// resolverSpec builds a two-state workflow whose destination is configured by
// the caller, so each test can vary only the thing under test.
func resolverSpec(dest *workflow.State) *workflow.Spec {
	return &workflow.Spec{
		Version: 2,
		Entry:   "origin",
		States: map[string]*workflow.State{
			"origin": {
				PromptTask: "origin",
				OnEvent:    map[string]string{"Go": "dest"},
			},
			"dest": dest,
		},
	}
}

// newPendingResolver returns a resolver whose transition tool has already been
// called, i.e. a transition is pending commit.
func newPendingResolver(t *testing.T, spec *workflow.Spec) *workflowStateResolver {
	t.Helper()

	machine := workflow.NewStateMachine(spec)
	transExec := workflow.NewTransitionExecutor(machine, spec)

	args := json.RawMessage(`{"event":"Go","context":"caller verified"}`)
	_, err := transExec.Execute(context.Background(), nil, args)
	require.NoError(t, err)
	require.NotNil(t, transExec.Pending(), "transition must be pending before commit")

	// Registry is nil: these cases must decide before reaching prompt rendering.
	return newWorkflowStateResolver(machine, spec, transExec, nil)
}

func TestWorkflowStateResolver_NoPendingTransition(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	machine := workflow.NewStateMachine(spec)
	transExec := workflow.NewTransitionExecutor(machine, spec)
	resolver := newWorkflowStateResolver(machine, spec, transExec, nil)

	handoff, err := resolver.ResolvePendingHandoff(context.Background())

	require.NoError(t, err)
	require.False(t, handoff.Changed, "no pending transition must not change the turn")
	require.Equal(t, "origin", machine.CurrentState())
}

func TestWorkflowStateResolver_NilExecutorIsInert(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	resolver := newWorkflowStateResolver(workflow.NewStateMachine(spec), spec, nil, nil)

	handoff, err := resolver.ResolvePendingHandoff(context.Background())

	require.NoError(t, err)
	require.False(t, handoff.Changed)
}

// The states the turn must not continue into. In every case the transition is
// still committed — only the in-turn continuation is suppressed, so the state
// machine has advanced even though the turn ends.
func TestWorkflowStateResolver_CommitsButDoesNotContinue(t *testing.T) {
	cases := []struct {
		name string
		dest *workflow.State
		why  string
	}{
		{
			name: "external orchestration",
			dest: &workflow.State{PromptTask: "dest", Orchestration: workflow.OrchestrationExternal},
			why:  "RFC 0009: the runtime pauses for an externally injected event",
		},
		{
			name: "composition orchestration",
			dest: &workflow.State{PromptTask: "dest", Orchestration: workflow.OrchestrationComposition},
			why:  "CompositionStage runs the state itself",
		},
		{
			name: "no prompt_task",
			dest: &workflow.State{},
			why:  "nothing to render",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := resolverSpec(tc.dest)
			resolver := newPendingResolver(t, spec)

			handoff, err := resolver.ResolvePendingHandoff(context.Background())

			require.NoError(t, err)
			require.False(t, handoff.Changed, tc.why)
			require.Equal(t, "dest", resolver.machine.CurrentState(),
				"the transition must still be committed")
			require.Nil(t, resolver.transExec.Pending(),
				"pending transition must be cleared by the commit")
		})
	}
}

// A destination that is missing from the spec is a config error, not a silent
// no-op — continuing under the origin prompt would hide it.
func TestWorkflowStateResolver_UnknownDestinationErrors(t *testing.T) {
	spec := &workflow.Spec{
		Version: 2,
		Entry:   "origin",
		States: map[string]*workflow.State{
			"origin": {PromptTask: "origin", OnEvent: map[string]string{"Go": "ghost"}},
		},
	}
	resolver := newPendingResolver(t, spec)

	_, err := resolver.ResolvePendingHandoff(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}

// Rendering without a registry is a hard error rather than a silent handoff to
// an empty prompt.
func TestWorkflowStateResolver_NoRegistryErrors(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	resolver := newPendingResolver(t, spec)

	_, err := resolver.ResolvePendingHandoff(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt registry")
}

func TestWorkflowStateResolver_CurrentStateMeta(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest", Description: "the destination"})
	machine := workflow.NewStateMachine(spec)
	resolver := newWorkflowStateResolver(machine, spec, nil, nil)

	meta := resolver.CurrentStateMeta()

	require.Equal(t, "origin", meta["current_state"])
	require.Equal(t, false, meta["terminal"], "origin has outgoing events")

	// After transitioning, the meta must follow the machine — this is what
	// makes per-message attribution correct across a multi-state turn.
	_, err := machine.ProcessEvent("Go")
	require.NoError(t, err)

	meta = resolver.CurrentStateMeta()
	require.Equal(t, "dest", meta["current_state"])
	require.Equal(t, "the destination", meta["description"])
	require.Equal(t, true, meta["terminal"], "dest has no outgoing events")
}

func TestWorkflowStateResolver_CurrentStateMetaNilMachine(t *testing.T) {
	resolver := &workflowStateResolver{}
	require.Nil(t, resolver.CurrentStateMeta())
}

// A nil *workflowResolverHolder is non-nil once boxed into the interface, so
// the pipeline installs it and the tool loop calls through on every round. Any
// Conversation built without a holder must therefore not panic.
func TestWorkflowResolverHolder_NilReceiverIsSafe(t *testing.T) {
	var holder *workflowResolverHolder

	require.NotPanics(t, func() {
		handoff, err := holder.ResolvePendingHandoff(context.Background())
		require.NoError(t, err)
		require.False(t, handoff.Changed)
		require.Nil(t, holder.CurrentStateMeta())
		holder.set(nil)
	})
}

// An empty holder (the plain-conversation case) is inert until populated.
func TestWorkflowResolverHolder_EmptyThenPopulated(t *testing.T) {
	holder := &workflowResolverHolder{}

	handoff, err := holder.ResolvePendingHandoff(context.Background())
	require.NoError(t, err)
	require.False(t, handoff.Changed, "empty holder must report no handoff")
	require.Nil(t, holder.CurrentStateMeta())

	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	machine := workflow.NewStateMachine(spec)
	holder.set(newWorkflowStateResolver(machine, spec, nil, nil))

	require.Equal(t, "origin", holder.CurrentStateMeta()["current_state"],
		"once populated the holder must delegate")
}
