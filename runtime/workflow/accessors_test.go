package workflow_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// terminal, max_visits and orchestration are pointers on the generated
// WorkflowState because the spec makes them optional. That is more correct than
// the plain values they replaced — absent is now distinguishable from the zero
// — but it means every reader has to resolve the documented default. These
// three accessors are where that happens, so it happens once.

// TestIsTerminalTreatsADeadEndAsTerminal — a state with no outgoing events ends
// the workflow whether or not it says so. Reading only the flag would leave the
// machine sitting on a state it can never leave.
func TestIsTerminalTreatsADeadEndAsTerminal(t *testing.T) {
	require.True(t, workflow.IsTerminal(nil), "a missing state cannot be run on")

	require.True(t, workflow.IsTerminal(&workflow.State{}),
		"no on_event means terminal even with the flag unset")

	require.True(t, workflow.IsTerminal(&workflow.State{
		Terminal: packspec.Ptr(true),
		OnEvent:  map[string]string{"Go": "next"},
	}), "an explicit terminal is terminal even with transitions declared")

	require.False(t, workflow.IsTerminal(&workflow.State{
		OnEvent: map[string]string{"Go": "next"},
	}), "a state with somewhere to go is not terminal")

	require.False(t, workflow.IsTerminal(&workflow.State{
		Terminal: packspec.Ptr(false),
		OnEvent:  map[string]string{"Go": "next"},
	}))
}

func TestMaxVisitsOf(t *testing.T) {
	require.Equal(t, 0, workflow.MaxVisitsOf(nil))
	require.Equal(t, 0, workflow.MaxVisitsOf(&workflow.State{}),
		"undeclared means uncapped, which is 0")
	require.Equal(t, 3, workflow.MaxVisitsOf(&workflow.State{MaxVisits: packspec.Ptr(3)}))
}

// TestOrchestrationOfDefaultsToInternal — the spec's documented default. A
// reader that saw "" instead would fall through every switch on orchestration
// and treat an ordinary state as none of the four modes.
func TestOrchestrationOfDefaultsToInternal(t *testing.T) {
	require.Equal(t, workflow.OrchestrationInternal, workflow.OrchestrationOf(nil))
	require.Equal(t, workflow.OrchestrationInternal,
		workflow.OrchestrationOf(&workflow.State{}))
	require.Equal(t, workflow.OrchestrationInternal,
		workflow.OrchestrationOf(&workflow.State{Orchestration: packspec.Ptr("")}),
		"an explicitly empty string is still undeclared")

	require.Equal(t, workflow.OrchestrationExternal,
		workflow.OrchestrationOf(&workflow.State{
			Orchestration: packspec.Ptr(workflow.OrchestrationExternal),
		}))
}
