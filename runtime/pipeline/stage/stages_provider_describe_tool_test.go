package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover the provider-stage half of #2326: ProviderConfig.DescribeTool
// gives a conversation its own view of a shared registry's descriptors on
// every tools build, and a change in that view mid-turn rebuilds the array.

// describedProvider calls workflow__transition on its first round, answers on
// the second, and records the description of "skill" each round was handed
// ("" when it was withheld). onFirstRound runs after round one's tools were
// built, standing in for state a tool call changes.
type describedProvider struct {
	*mock.ToolProvider
	seen         []string
	onFirstRound func()
}

func (p *describedProvider) SupportsStreaming() bool { return false }

func (p *describedProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, pt providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	desc := ""
	descriptors, _ := pt.([]*providers.ToolDescriptor)
	for _, d := range descriptors {
		if d.Name == "skill" {
			desc = d.Description
		}
	}
	p.seen = append(p.seen, desc)
	if len(p.seen) == 1 {
		if p.onFirstRound != nil {
			p.onFirstRound()
		}
		calls := []types.MessageToolCall{{
			ID: "call-1", Name: "workflow__transition", Args: []byte(`{"event":"Next"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// funcResolver reports one unchanging state, but runs commit on each call —
// the moment a real resolver commits a pending transition.
type funcResolver struct {
	commit func()
}

func (r *funcResolver) ResolveCurrentState(_ context.Context) (Handoff, error) {
	r.commit()
	return Handoff{
		Valid: true, SystemPrompt: "SAME PROMPT", AllowedTools: []string{"workflow__transition", "skill"},
	}, nil
}

func (r *funcResolver) CurrentStateMeta() map[string]any { return nil }

// newDescribeToolStage builds a stage over a registry holding
// workflow__transition and a capability-namespaced "skill" tool, whose
// description DescribeTool replaces with *view, or withholds when *view is "".
func newDescribeToolStage(t *testing.T, view *string, prov *describedProvider) (*ProviderStage, *tools.Registry) {
	t.Helper()
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "workflow__transition", Description: "Transition", InputSchema: []byte(`{"type":"object"}`),
	}))
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "skill", Description: "registered", InputSchema: []byte(`{"type":"object"}`),
	}))

	turnState := NewTurnState()
	turnState.SystemPrompt = "SAME PROMPT"
	turnState.AllowedTools = []string{"workflow__transition", "skill"}

	describe := func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
		if d.Name != "skill" {
			return d
		}
		if *view == "" {
			return nil
		}
		out := *d
		out.Description = *view
		return &out
	}
	stage := NewProviderStageWithTurnState(prov, registry, nil,
		&ProviderConfig{MaxTokens: 100, DescribeTool: describe}, nil, nil, turnState)
	return stage, registry
}

func runDescribeToolTurn(t *testing.T, stage *ProviderStage) {
	t.Helper()
	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "go"}
	input <- NewMessageElement(&userMsg)
	close(input)
	output := make(chan StreamElement, 32)
	require.NoError(t, stage.Process(context.Background(), input, output))
	for range output { //nolint:revive // draining
	}
}

// A tool round that changes what DescribeTool returns rebuilds the array for
// the next round, though no grant or exclusion changed — and the registry's
// own descriptor is never touched.
func TestDescribeTool_ChangeAfterToolRoundRebuildsTools(t *testing.T) {
	view := "first view"
	prov := &describedProvider{ToolProvider: mock.NewToolProvider("m", "m", false, nil)}
	prov.onFirstRound = func() { view = "second view" }
	stage, registry := newDescribeToolStage(t, &view, prov)

	runDescribeToolTurn(t, stage)

	require.Len(t, prov.seen, 2)
	assert.Equal(t, "first view", prov.seen[0])
	assert.Equal(t, "second view", prov.seen[1], "the round after the change must see the new description")
	assert.Equal(t, "registered", registry.Get("skill").Description, "the shared descriptor must not be mutated")
}

// A mid-turn handoff to a state with the same prompt still rebuilds the array
// when the commit changed what DescribeTool returns — here, withholding the
// tool, as a workflow state with `skills: none` withholds skill__activate.
func TestDescribeTool_HandoffWithSamePromptRebuildsTools(t *testing.T) {
	view := "first view"
	prov := &describedProvider{ToolProvider: mock.NewToolProvider("m", "m", false, nil)}
	// The transition is pending once round one's tool call has run, and
	// commits when the next round resolves the state — after the post-round
	// check, so only the handoff can catch it.
	pending := false
	prov.onFirstRound = func() { pending = true }
	stage, _ := newDescribeToolStage(t, &view, prov)
	stage.SetWorkflowStateResolver(&funcResolver{commit: func() {
		if pending {
			view = ""
		}
	}})

	runDescribeToolTurn(t, stage)

	require.Len(t, prov.seen, 2)
	assert.Equal(t, "first view", prov.seen[0])
	assert.Empty(t, prov.seen[1], "the round after the commit must not be offered the withheld tool")
}

// Without DescribeTool the stage keeps no fingerprint and never reports a
// change, so a build is only redone for grants or exclusions.
func TestDescribedToolsChanged_FalseWithoutDescribeTool(t *testing.T) {
	s := &ProviderStage{config: &ProviderConfig{}}
	assert.Empty(t, s.describedToolsKey([]*providers.ToolDescriptor{{Name: "a", Description: "d"}}))
	assert.False(t, s.describedToolsChanged([]string{"a"}, nil))
}
