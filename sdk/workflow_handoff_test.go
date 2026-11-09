package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// handoffSDKProvider records the system prompt per round and transitions on the
// first round, so a single Send spans two workflow states.
type handoffSDKProvider struct {
	*mock.ToolProvider
	seenPrompts []string
}

// Pin to the unary tool loop for determinism.
func (p *handoffSDKProvider) SupportsStreaming() bool { return false }

func (p *handoffSDKProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)
	return providers.PredictionResponse{Content: "no tools available"}, nil
}

func (p *handoffSDKProvider) PredictWithTools(
	_ context.Context,
	req providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)

	if len(p.seenPrompts) == 1 {
		calls := []types.MessageToolCall{{
			ID:   "call-1",
			Name: "workflow__transition",
			Args: []byte(`{"event":"InfoComplete","context":"name and account collected"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "destination speaking"}, nil, nil
}

// The end-to-end guarantee. One Send: the LLM transitions intake -> processing,
// and the PROCESSING state produces the reply, in the same turn, with no second
// user message. Before this, Send returned the intake state's text and the
// processing state stayed silent until the user typed again.
func TestWorkflowConversation_DestinationStateSpeaksInSameSend(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	provider := &handoffSDKProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}

	wc, err := OpenWorkflow(packPath, WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { require.NoError(t, wc.Close()) }()

	require.Equal(t, "intake", wc.CurrentState())

	resp, err := wc.Send(context.Background(), "I need help with my account")
	require.NoError(t, err)

	require.Equal(t, "processing", wc.CurrentState(),
		"the transition must have been committed")

	require.Len(t, provider.seenPrompts, 2,
		"the transition tool call must produce a second round")
	require.Equal(t, "You gather information.", provider.seenPrompts[0],
		"round 1 runs as the origin state")
	require.Equal(t, "You process requests.", provider.seenPrompts[1],
		"round 2 must run as the DESTINATION state -- this is the fix")

	require.Contains(t, resp.Text(), "destination speaking",
		"Send must return the destination state's reply, not the origin's")
}

// A turn with no transition must behave exactly as before: one round, one
// state, origin prompt throughout.
func TestWorkflowConversation_NoTransitionIsUnchanged(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	provider := &handoffSDKProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}
	// Consume the scripted transition so the first real round answers plainly.
	provider.seenPrompts = append(provider.seenPrompts, "PRIMED")

	wc, err := OpenWorkflow(packPath, WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { require.NoError(t, wc.Close()) }()

	_, err = wc.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Equal(t, "intake", wc.CurrentState(), "state must not move")
	require.Len(t, provider.seenPrompts, 2, "primed entry plus one real round")
	require.Equal(t, "You gather information.", provider.seenPrompts[1])
}

// orchArtifactHandoffPackJSON hands off from intake to a state whose prompt
// reads an artifact that intake declares.
const orchArtifactHandoffPackJSON = `{
	"id": "wf-artifact", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"prompts": {
		"gather_info": {"id": "gather_info", "name": "g", "version": "1.0.0", "system_template": "You gather."},
		"process": {"id": "process", "name": "p", "version": "1.0.0",
			"system_template": "You process. Summary: {{artifacts.summary}}"}
	},
	"workflow": {"version": 2, "entry": "intake", "states": {
		"intake": {"prompt_task": "gather_info", "on_event": {"InfoComplete": "processing"},
			"artifacts": {"summary": {"type": "text/plain"}}},
		"processing": {"prompt_task": "process"}
	}}
}`

// orchArtifactHandoffProvider makes the given calls on round one, one per
// round, then answers in text. It records the system prompt of every round.
type orchArtifactHandoffProvider struct {
	*mock.ToolProvider
	calls       []types.MessageToolCall
	seenPrompts []string
}

func (p *orchArtifactHandoffProvider) SupportsStreaming() bool { return false }

func (p *orchArtifactHandoffProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)
	if n := len(p.seenPrompts); n <= len(p.calls) {
		call := []types.MessageToolCall{p.calls[n-1]}
		return providers.PredictionResponse{ToolCalls: call}, call, nil
	}
	return providers.PredictionResponse{Content: "destination speaking"}, nil, nil
}

func orchTransitionCall() types.MessageToolCall {
	return types.MessageToolCall{ID: "call-t", Name: "workflow__transition",
		Args: []byte(`{"event":"InfoComplete","context":"collected"}`)}
}

// A mid-turn handoff to a state whose prompt reads an artifact nobody has set
// fails the handoff round with ErrTemplateUnresolved naming the placeholder;
// the destination round never reaches the model. This pins the v2.19.0
// behavior (#2549). Whether it should instead render the artifact empty is an
// open decision; change this test only with that decision.
func TestWorkflowHandoff_UnsetArtifactFailsTheHandoffRound(t *testing.T) {
	provider := &orchArtifactHandoffProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		calls:        []types.MessageToolCall{orchTransitionCall()},
	}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, orchArtifactHandoffPackJSON),
		WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { _ = wc.Close() }()

	_, err = wc.Send(context.Background(), "help")
	require.ErrorIs(t, err, stage.ErrTemplateUnresolved)
	assert.Contains(t, err.Error(), "artifacts.summary")
	assert.Equal(t, []string{"You gather."}, provider.seenPrompts,
		"the destination round, with its literal placeholder, never reaches the model")
}

// The same handoff after the origin state set the artifact renders it into the
// destination's prompt on the handoff round.
func TestWorkflowHandoff_SetArtifactRendersOnTheHandoffRound(t *testing.T) {
	provider := &orchArtifactHandoffProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		calls: []types.MessageToolCall{
			{ID: "call-a", Name: "workflow__set_artifact", Args: []byte(`{"name":"summary","value":"two refunds"}`)},
			orchTransitionCall(),
		},
	}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, orchArtifactHandoffPackJSON),
		WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { _ = wc.Close() }()

	resp, err := wc.Send(context.Background(), "help")
	require.NoError(t, err)
	assert.Equal(t, "processing", wc.CurrentState())
	assert.Equal(t, []string{"You gather.", "You gather.", "You process. Summary: two refunds"}, provider.seenPrompts)
	assert.Equal(t, "destination speaking", resp.Text())
}
