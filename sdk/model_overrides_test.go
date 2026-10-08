package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// A prompt's model_overrides entry applies when the provider that runs the
// prompt has that model (#2201). Before, every template load passed an empty
// model, so no override ever applied.

// overrideProvider is a mock LLM with a chosen model that records the system
// prompt and parameters of its last request.
type overrideProvider struct {
	providers.Provider
	mu   sync.Mutex
	last providers.PredictionRequest
	n    int
}

func newOverrideProvider(id, model string) *overrideProvider {
	return &overrideProvider{Provider: mock.NewProviderWithRepository(id, model, false,
		mock.NewInMemoryMockRepository("ok"))}
}

func (p *overrideProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	p.record(req)
	return p.Provider.Predict(ctx, req)
}

func (p *overrideProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.Provider.PredictStream(ctx, req)
}

func (p *overrideProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = req
	p.n++
}

func (p *overrideProvider) lastRequest() (providers.PredictionRequest, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last, p.n
}

const modelOverridesPack = `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "model-overrides",
	"name": "model-overrides",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"requires": {"providers": [
		{"key": "default", "role": "llm", "required": true},
		{"key": "drafter", "role": "llm", "required": true}
	]},
	"prompts": {
		"chat": {
			"id": "chat", "name": "chat", "version": "1.0.0",
			"system_template": "chat base",
			"parameters": {"max_tokens": 500, "temperature": 0.9},
			"model_overrides": {
				"agent-model": {
					"system_template_prefix": "AGENT-PREFIX ",
					"system_template_suffix": " AGENT-SUFFIX",
					"parameters": {"max_tokens": 77, "temperature": 0.1}
				}
			}
		},
		"draft": {
			"id": "draft", "name": "draft", "version": "1.0.0",
			"system_template": "draft base", "provider": "drafter",
			"parameters": {"max_tokens": 300, "temperature": 0.8},
			"model_overrides": {
				"agent-model":   {"system_template": "WRONG: the agent's override", "parameters": {"max_tokens": 1}},
				"drafter-model": {"system_template": "drafter override", "parameters": {"temperature": 0.4}}
			}
		}
	},
	"workflow": {
		"version": 1,
		"entry": "compose",
		"states": {"compose": {"orchestration": "composition", "composition": "flow", "terminal": true}}
	},
	"compositions": {
		"flow": {"version": 1, "output": "s",
			"steps": [{"id": "s", "kind": "prompt", "prompt_task": "draft", "input": "${input}"}]}
	}
}`

func TestModelOverrides_ApplyForTheRunningProvidersModel(t *testing.T) {
	packPath := createTestPackFile(t, modelOverridesPack)
	ctx := context.Background()

	t.Run("the agent's model selects the prompt's override", func(t *testing.T) {
		agent := newOverrideProvider("agent", "agent-model")
		conv, err := Open(packPath, "chat", WithProvider(agent),
			withPooledProvider(newOverrideProvider("drafter", "drafter-model")))
		require.NoError(t, err)
		defer conv.Close()
		_, err = conv.Send(ctx, "hi")
		require.NoError(t, err)

		req, n := agent.lastRequest()
		require.Equal(t, 1, n)
		assert.Equal(t, "AGENT-PREFIX chat base AGENT-SUFFIX", req.System)
		assert.Equal(t, 77, req.MaxTokens, "the override's parameters replace the prompt's")
		assert.InDelta(t, 0.1, req.Temperature, 1e-6)
	})
	t.Run("a model with no override gets the base prompt and parameters", func(t *testing.T) {
		agent := newOverrideProvider("agent", "other-model")
		conv, err := Open(packPath, "chat", WithProvider(agent),
			withPooledProvider(newOverrideProvider("drafter", "drafter-model")))
		require.NoError(t, err)
		defer conv.Close()
		_, err = conv.Send(ctx, "hi")
		require.NoError(t, err)

		req, _ := agent.lastRequest()
		assert.Equal(t, "chat base", req.System)
		assert.Equal(t, 500, req.MaxTokens)
	})
	t.Run("a prompt run on a bound provider uses that provider's model", func(t *testing.T) {
		drafter := newOverrideProvider("drafter", "drafter-model")
		conv, err := Open(packPath, "draft", WithProvider(newOverrideProvider("agent", "agent-model")),
			withPooledProvider(drafter))
		require.NoError(t, err)
		defer conv.Close()
		_, err = conv.Send(ctx, "hi")
		require.NoError(t, err)

		req, _ := drafter.lastRequest()
		assert.Equal(t, "drafter override", req.System, "not the agent's override")
	})
	t.Run("a composition step uses its own provider's model", func(t *testing.T) {
		drafter := newOverrideProvider("drafter", "drafter-model")
		wc, err := OpenWorkflow(packPath, WithProvider(newOverrideProvider("agent", "agent-model")),
			withPooledProvider(drafter))
		require.NoError(t, err)
		defer wc.Close()
		_, err = wc.Send(ctx, "hi")
		require.NoError(t, err)

		req, n := drafter.lastRequest()
		require.Equal(t, 1, n)
		assert.Equal(t, "drafter override", req.System, "not the agent's override")
		assert.Equal(t, 300, req.MaxTokens, "a composition step carries its prompt's parameters (#2205)")
		assert.InDelta(t, 0.4, req.Temperature, 1e-6, "and its override's for the step provider's model")
	})
}
