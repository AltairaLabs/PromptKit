package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

func threeStageSpec() *workflow.Spec {
	return &workflow.Spec{
		Entry: "extract",
		States: map[string]*workflow.State{
			"extract":    {PromptTask: "extract"},
			"categorize": {PromptTask: "categorize"},
			"compose":    {PromptTask: "compose"},
		},
	}
}

func TestResolveWorkflowStartState_EmptyTaskTypeDefaultsToEntry(t *testing.T) {
	got, err := resolveWorkflowStartState(threeStageSpec(), "")
	require.NoError(t, err)
	assert.Equal(t, "extract", got)
}

func TestResolveWorkflowStartState_EntryMatch(t *testing.T) {
	got, err := resolveWorkflowStartState(threeStageSpec(), "extract")
	require.NoError(t, err)
	assert.Equal(t, "extract", got)
}

func TestResolveWorkflowStartState_NonEntryStage(t *testing.T) {
	got, err := resolveWorkflowStartState(threeStageSpec(), "compose")
	require.NoError(t, err)
	assert.Equal(t, "compose", got, "should pin the non-entry stage")
}

func TestResolveWorkflowStartState_NoMatchErrors(t *testing.T) {
	_, err := resolveWorkflowStartState(threeStageSpec(), "composer") // typo
	require.Error(t, err)
	assert.Contains(t, err.Error(), "composer")
}

// Entry match wins even when another state shares the entry's prompt_task,
// keeping the common case unambiguous.
func TestResolveWorkflowStartState_EntryWinsOnSharedPromptTask(t *testing.T) {
	spec := &workflow.Spec{
		Entry: "extract",
		States: map[string]*workflow.State{
			"extract": {PromptTask: "shared"},
			"other":   {PromptTask: "shared"},
		},
	}
	got, err := resolveWorkflowStartState(spec, "shared")
	require.NoError(t, err)
	assert.Equal(t, "extract", got)
}

func TestResolveWorkflowStartState_AmbiguousNonEntryErrors(t *testing.T) {
	spec := &workflow.Spec{
		Entry: "extract",
		States: map[string]*workflow.State{
			"extract": {PromptTask: "extract"},
			"a":       {PromptTask: "dup"},
			"b":       {PromptTask: "dup"},
		},
	}
	_, err := resolveWorkflowStartState(spec, "dup")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}
