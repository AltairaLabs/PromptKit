package prompt

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool       { return &b }
func float64Ptr(f float64) *float64 { return &f }

func TestPack_BackwardCompatibility_NoEvals(t *testing.T) {
	// A pack JSON without the "evals" field should still load correctly.
	raw := `{
		"id": "test-pack",
		"name": "Test Pack",
		"version": "v1.0.0",
		"description": "A test pack",
		"template_engine": {"version": "v1", "syntax": "handlebars"},
		"prompts": {
			"greeting": {
				"id": "greeting",
				"name": "Greeting",
				"description": "Say hello",
				"version": "1.0.0",
				"system_template": "Hello {{name}}"
			}
		}
	}`

	var pack Pack
	err := json.Unmarshal([]byte(raw), &pack)
	require.NoError(t, err)

	assert.Equal(t, "test-pack", pack.ID)
	assert.Nil(t, pack.Evals)
	assert.Nil(t, pack.Prompts["greeting"].Evals)
}

func TestPack_PackLevelEvals(t *testing.T) {
	raw := `{
		"id": "test-pack",
		"name": "Test Pack",
		"version": "v1.0.0",
		"description": "A test pack",
		"template_engine": {"version": "v1", "syntax": "handlebars"},
		"prompts": {
			"greeting": {
				"id": "greeting",
				"name": "Greeting",
				"description": "Say hello",
				"version": "1.0.0",
				"system_template": "Hello {{name}}"
			}
		},
		"evals": [
			{
				"id": "tone-check",
				"type": "llm_judge",
				"trigger": "every_turn",
				"params": {"criteria": "professional tone"},
				"description": "Check professional tone"
			}
		]
	}`

	var pack Pack
	err := json.Unmarshal([]byte(raw), &pack)
	require.NoError(t, err)

	require.Len(t, pack.Evals, 1)
	assert.Equal(t, "tone-check", pack.Evals[0].ID)
	assert.Equal(t, "llm_judge", pack.Evals[0].Type)
	assert.Equal(t, evals.TriggerEveryTurn, pack.Evals[0].Trigger)
	assert.Equal(t, "professional tone", pack.Evals[0].Params["criteria"])
	assert.Equal(t, "Check professional tone", pack.Evals[0].Description)
}

func TestPack_PromptLevelEvals(t *testing.T) {
	raw := `{
		"id": "test-pack",
		"name": "Test Pack",
		"version": "v1.0.0",
		"description": "A test pack",
		"template_engine": {"version": "v1", "syntax": "handlebars"},
		"prompts": {
			"greeting": {
				"id": "greeting",
				"name": "Greeting",
				"description": "Say hello",
				"version": "1.0.0",
				"system_template": "Hello {{name}}",
				"evals": [
					{
						"id": "length-check",
						"type": "deterministic",
						"trigger": "every_turn",
						"params": {"max_length": 500},
						"enabled": false,
						"sample_percentage": 10.0
					}
				]
			}
		}
	}`

	var pack Pack
	err := json.Unmarshal([]byte(raw), &pack)
	require.NoError(t, err)

	assert.Nil(t, pack.Evals)

	prompt := pack.Prompts["greeting"]
	require.NotNil(t, prompt)
	require.Len(t, prompt.Evals, 1)

	eval := prompt.Evals[0]
	assert.Equal(t, "length-check", eval.ID)
	assert.Equal(t, "deterministic", eval.Type)
	assert.Equal(t, evals.TriggerEveryTurn, eval.Trigger)
	assert.Equal(t, float64(500), eval.Params["max_length"])
	assert.False(t, eval.IsEnabled())
	assert.Equal(t, 10.0, eval.GetSamplePercentage())
}

func TestPack_EvalsRoundTrip(t *testing.T) {
	original := &Pack{
		ID:          "round-trip",
		Name:        "Round Trip",
		Version:     "v1.0.0",
		Description: "Test round-trip",
		TemplateEngine: &TemplateEngineInfo{
			Version: "v1",
			Syntax:  "handlebars",
		},
		Prompts: map[string]*PackPrompt{
			"chat": {
				ID:             "chat",
				Name:           "Chat",
				Description:    "Chat prompt",
				Version:        "1.0.0",
				SystemTemplate: "You are a helper",
				Evals: []evals.EvalDef{
					{
						ID:      "prompt-eval",
						Type:    "deterministic",
						Trigger: evals.TriggerOnSessionComplete,
						Params:  map[string]any{"threshold": 0.8},
					},
				},
			},
		},
		Evals: []evals.EvalDef{
			{
				ID:               "pack-eval",
				Type:             "llm_judge",
				Trigger:          evals.TriggerSampleTurns,
				Params:           map[string]any{"criteria": "helpful"},
				Enabled:          boolPtr(true),
				SamplePercentage: float64Ptr(25.0),
				Metric: &evals.MetricDef{
					Name: "helpfulness",
					Type: evals.MetricGauge,
				},
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Pack
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Pack-level evals
	require.Len(t, restored.Evals, 1)
	assert.Equal(t, "pack-eval", restored.Evals[0].ID)
	assert.Equal(t, "llm_judge", restored.Evals[0].Type)
	assert.Equal(t, evals.TriggerSampleTurns, restored.Evals[0].Trigger)
	assert.True(t, restored.Evals[0].IsEnabled())
	assert.Equal(t, 25.0, restored.Evals[0].GetSamplePercentage())
	require.NotNil(t, restored.Evals[0].Metric)
	assert.Equal(t, "helpfulness", restored.Evals[0].Metric.Name)
	assert.Equal(t, evals.MetricGauge, restored.Evals[0].Metric.Type)

	// Prompt-level evals
	chatPrompt := restored.Prompts["chat"]
	require.NotNil(t, chatPrompt)
	require.Len(t, chatPrompt.Evals, 1)
	assert.Equal(t, "prompt-eval", chatPrompt.Evals[0].ID)
	assert.Equal(t, evals.TriggerOnSessionComplete, chatPrompt.Evals[0].Trigger)
}

func TestPack_EvalsOmittedWhenEmpty(t *testing.T) {
	pack := &Pack{
		ID:      "no-evals",
		Name:    "No Evals",
		Version: "v1.0.0",
		Prompts: map[string]*PackPrompt{
			"basic": {
				ID:             "basic",
				Name:           "Basic",
				Version:        "1.0.0",
				SystemTemplate: "Hello",
			},
		},
	}

	data, err := json.Marshal(pack)
	require.NoError(t, err)

	// The "evals" key should not appear in the JSON output
	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	_, hasPackEvals := raw["evals"]
	assert.False(t, hasPackEvals, "evals key should be omitted from pack JSON when nil")

	var promptsRaw map[string]json.RawMessage
	err = json.Unmarshal(raw["prompts"], &promptsRaw)
	require.NoError(t, err)

	var basicRaw map[string]json.RawMessage
	err = json.Unmarshal(promptsRaw["basic"], &basicRaw)
	require.NoError(t, err)

	_, hasPromptEvals := basicRaw["evals"]
	assert.False(t, hasPromptEvals, "evals key should be omitted from prompt JSON when nil")
}
