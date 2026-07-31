package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
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

// testRegistry builds a prompt registry with one template per named task.
func testRegistry(t *testing.T, templates map[string]string) *prompt.Registry {
	t.Helper()

	repo := memory.NewPromptRepository()
	for taskType, tmpl := range templates {
		repo.RegisterPrompt(taskType, &prompt.Config{
			APIVersion: "promptkit.altairalabs.ai/v1alpha1",
			Kind:       "Prompt",
			Spec: prompt.Spec{
				TaskType:       taskType,
				Version:        "1.0.0",
				SystemTemplate: tmpl,
			},
		})
	}
	return prompt.NewRegistryWithRepository(repo)
}

// With nothing pending, the resolver reports the state the workflow is already
// in. It is not a change notification -- a re-executed pipeline depends on
// getting the current state back every time.
func TestWorkflowStateResolver_ReportsCurrentStateWithNothingPending(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	machine := workflow.NewStateMachine(spec)
	transExec := workflow.NewTransitionExecutor(machine, spec)
	registry := testRegistry(t, map[string]string{"origin": "ORIGIN PROMPT"})
	resolver := newWorkflowStateResolver(machine, spec, transExec, registry)

	// Repeated calls must be idempotent: the loop calls this every round.
	for i := range 3 {
		handoff, err := resolver.ResolveCurrentState(context.Background())
		require.NoError(t, err, "call %d", i)
		require.True(t, handoff.Valid)
		require.Equal(t, "ORIGIN PROMPT", handoff.SystemPrompt)
	}
	require.Equal(t, "origin", machine.CurrentState(), "state must not move")
}

// After the transition tool has run, the resolver reports the destination --
// and keeps reporting it, which is what lets a resumed execution recover.
func TestWorkflowStateResolver_ReportsDestinationAfterCommit(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	registry := testRegistry(t, map[string]string{
		"origin": "ORIGIN PROMPT",
		"dest":   "DESTINATION PROMPT",
	})
	resolver := newPendingResolver(t, spec)
	resolver.registry = registry

	handoff, err := resolver.ResolveCurrentState(context.Background())
	require.NoError(t, err)
	require.True(t, handoff.Valid)
	require.Equal(t, "DESTINATION PROMPT", handoff.SystemPrompt)
	require.Equal(t, "dest", resolver.machine.CurrentState())

	// Nothing pending now, but the answer must not revert.
	handoff, err = resolver.ResolveCurrentState(context.Background())
	require.NoError(t, err)
	require.Equal(t, "DESTINATION PROMPT", handoff.SystemPrompt,
		"a later call must still report the destination -- this is what recovers a resumed turn")
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

	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := resolverSpec(tc.dest)
			resolver := newPendingResolver(t, spec)

			handoff, err := resolver.ResolveCurrentState(context.Background())

			require.NoError(t, err)
			require.True(t, handoff.Stop, tc.why)
			require.False(t, handoff.Valid)
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

	_, err := resolver.ResolveCurrentState(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}

// Rendering without a registry is a hard error rather than a silent handoff to
// an empty prompt.
func TestWorkflowStateResolver_NoRegistryErrors(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	resolver := newPendingResolver(t, spec)

	_, err := resolver.ResolveCurrentState(context.Background())

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
		handoff, err := holder.ResolveCurrentState(context.Background())
		require.NoError(t, err)
		require.False(t, handoff.Valid)
		require.Nil(t, holder.CurrentStateMeta())
		holder.set(nil)
	})
}

// An empty holder (the plain-conversation case) is inert until populated.
func TestWorkflowResolverHolder_EmptyThenPopulated(t *testing.T) {
	holder := &workflowResolverHolder{}

	handoff, err := holder.ResolveCurrentState(context.Background())
	require.NoError(t, err)
	require.False(t, handoff.Valid, "empty holder must report no state")
	require.Nil(t, holder.CurrentStateMeta())

	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	machine := workflow.NewStateMachine(spec)
	holder.set(newWorkflowStateResolver(machine, spec, nil, nil))

	require.Equal(t, "origin", holder.CurrentStateMeta()["current_state"],
		"once populated the holder must delegate")
}

// A state with no prompt_task has nothing to render. The turn keeps whatever
// prompt it already has rather than being blanked or stopped.
func TestWorkflowStateResolver_NoPromptTaskLeavesTurnAlone(t *testing.T) {
	spec := resolverSpec(&workflow.State{})
	resolver := newPendingResolver(t, spec)

	handoff, err := resolver.ResolveCurrentState(context.Background())

	require.NoError(t, err)
	require.False(t, handoff.Valid)
	require.False(t, handoff.Stop)
	require.Equal(t, "dest", resolver.machine.CurrentState(),
		"the transition must still be committed")
}

// An externally orchestrated state still handles ordinary user turns. Only
// auto-advancing INTO it mid-turn is suppressed -- treating it as never-run
// ends a plain Send with no rounds and an empty response.
func TestWorkflowStateResolver_ExternalStateStillServesItsOwnTurns(t *testing.T) {
	spec := resolverSpec(&workflow.State{PromptTask: "dest"})
	spec.States["origin"].Orchestration = workflow.OrchestrationExternal
	machine := workflow.NewStateMachine(spec)
	registry := testRegistry(t, map[string]string{"origin": "ORIGIN PROMPT"})
	resolver := newWorkflowStateResolver(machine, spec, nil, registry)

	handoff, err := resolver.ResolveCurrentState(context.Background())

	require.NoError(t, err)
	require.False(t, handoff.Stop, "no transition committed: the state serves this turn")
	require.True(t, handoff.Valid)
	require.Equal(t, "ORIGIN PROMPT", handoff.SystemPrompt)
}
