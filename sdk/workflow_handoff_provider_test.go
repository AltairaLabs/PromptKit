package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A mid-turn workflow handoff runs the destination state's prompt on that
// prompt's provider and parameters, exactly as opening the state would
// (#2206). Before, the rest of the turn stayed on the origin's provider.

// handoffRoundProvider records each round it serves, and on its first round
// optionally calls the workflow transition tool.
type handoffRoundProvider struct {
	*mock.ToolProvider
	transitionEvent string

	mu     sync.Mutex
	rounds []providers.PredictionRequest
}

func newHandoffRoundProvider(id, model, transitionEvent string) *handoffRoundProvider {
	return &handoffRoundProvider{
		ToolProvider:    mock.NewToolProvider(id, model, false, nil),
		transitionEvent: transitionEvent,
	}
}

// Pin to the unary tool loop for determinism.
func (p *handoffRoundProvider) SupportsStreaming() bool { return false }

func (p *handoffRoundProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return providers.PredictionResponse{Content: "reply from " + p.ID()}, nil
}

func (p *handoffRoundProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if n := p.record(req); n == 1 && p.transitionEvent != "" {
		calls := []types.MessageToolCall{{
			ID: "call-1", Name: "workflow__transition",
			Args: []byte(`{"event":"` + p.transitionEvent + `","context":"handing over"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "reply from " + p.ID()}, nil, nil
}

func (p *handoffRoundProvider) record(req providers.PredictionRequest) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rounds = append(p.rounds, req)
	return len(p.rounds)
}

func (p *handoffRoundProvider) served() []providers.PredictionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providers.PredictionRequest(nil), p.rounds...)
}

const handoffProviderPack = `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "handoff-provider",
	"name": "handoff-provider",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"requires": {"providers": [
		{"key": "default", "role": "llm", "required": true},
		{"key": "drafter", "role": "llm", "required": true}
	]},
	"prompts": {
		"intake": {"id": "intake", "name": "intake", "version": "1.0.0",
			"system_template": "intake base",
			"parameters": {"max_tokens": 500, "temperature": 0.9}},
		"draft": {"id": "draft", "name": "draft", "version": "1.0.0",
			"system_template": "draft base", "provider": "drafter",
			"parameters": {"max_tokens": 123, "temperature": 0.2, "top_p": 0.6, "presence_penalty": 0.5, "top_k": 7},
			"model_overrides": {
				"agent-model":   {"system_template_suffix": " WRONG: the agent's override"},
				"drafter-model": {"system_template_suffix": " for drafter", "parameters": {"max_tokens": 64}}
			}}
	},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake":   {"prompt_task": "intake", "on_event": {"Draft": "drafting"}},
			"drafting": {"prompt_task": "draft", "terminal": true}
		}
	}
}`

func TestWorkflowHandoff_SwitchesToTheDestinationPromptsProvider(t *testing.T) {
	packPath := createTestPackFile(t, handoffProviderPack)
	agent := newHandoffRoundProvider("agent", "agent-model", "Draft")
	drafter := newHandoffRoundProvider("drafter", "drafter-model", "")

	wc, err := OpenWorkflow(packPath, WithProvider(agent), withPooledProvider(drafter))
	require.NoError(t, err)
	defer wc.Close()

	resp, err := wc.Send(context.Background(), "please draft it")
	require.NoError(t, err)
	require.Equal(t, "drafting", wc.CurrentState())

	agentRounds := agent.served()
	require.Len(t, agentRounds, 1, "the agent serves only the origin state's round")
	assert.Equal(t, "intake base", agentRounds[0].System)
	assert.Equal(t, 500, agentRounds[0].MaxTokens)

	drafterRounds := drafter.served()
	require.Len(t, drafterRounds, 1, "the destination's round runs on drafter, in the same Send")
	assert.Equal(t, "draft base for drafter", drafterRounds[0].System,
		"rendered with drafter's model_overrides entry, not the agent's")
	assert.Equal(t, 64, drafterRounds[0].MaxTokens, "the override's max_tokens")
	assert.InDelta(t, 0.2, drafterRounds[0].Temperature, 1e-6, "the destination prompt's temperature")
	assert.InDelta(t, 0.6, drafterRounds[0].TopP, 1e-6, "and its top_p")
	require.NotNil(t, drafterRounds[0].PresencePenalty, "and its presence_penalty")
	assert.InDelta(t, 0.5, *drafterRounds[0].PresencePenalty, 1e-6)
	require.NotNil(t, drafterRounds[0].TopK, "and its top_k")
	assert.Equal(t, 7, *drafterRounds[0].TopK)
	assert.Equal(t, "reply from drafter", resp.Text())
}

// A handoff between states on the same provider keeps it, and only the prompt
// and parameters change.
func TestWorkflowHandoff_SameProviderKeepsIt(t *testing.T) {
	pack := `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "handoff-same", "name": "handoff-same", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"a": {"id": "a", "name": "a", "version": "1.0.0", "system_template": "A", "parameters": {"max_tokens": 10}},
		"b": {"id": "b", "name": "b", "version": "1.0.0", "system_template": "B", "parameters": {"max_tokens": 20}}
	},
	"workflow": {"version": 1, "entry": "a", "states": {
		"a": {"prompt_task": "a", "on_event": {"Next": "b"}},
		"b": {"prompt_task": "b", "terminal": true}
	}}
}`
	agent := newHandoffRoundProvider("agent", "agent-model", "Next")
	wc, err := OpenWorkflow(createTestPackFile(t, pack), WithProvider(agent))
	require.NoError(t, err)
	defer wc.Close()

	_, err = wc.Send(context.Background(), "go")
	require.NoError(t, err)

	rounds := agent.served()
	require.Len(t, rounds, 2)
	assert.Equal(t, []string{"A", "B"}, []string{rounds[0].System, rounds[1].System})
	assert.Equal(t, []int{10, 20}, []int{rounds[0].MaxTokens, rounds[1].MaxTokens})
}
