package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateScenarioSchema generates the JSON Schema for Scenario configuration
func GenerateScenarioSchema() (interface{}, error) {
	return Generate(&SchemaConfig{
		Target:      &config.ScenarioConfig{},
		Filename:    "scenario.json",
		Title:       "PromptArena Scenario Configuration",
		Description: "Scenario configuration for PromptArena test cases",
		Customize: func(schema *jsonschema.Schema) {
			addScenarioOneOf(schema)
			addScenarioExample(schema)
		},
	})
}

// addScenarioOneOf adds a oneOf constraint to the Scenario definition
// to enforce mutual exclusivity between regular and workflow scenarios.
func addScenarioOneOf(schema *jsonschema.Schema) {
	scenarioDef, ok := schema.Definitions["Scenario"]
	if !ok {
		return
	}

	scenarioDef.OneOf = []*jsonschema.Schema{
		{
			Required:    []string{"task_type", "turns"},
			Description: "Regular conversation scenario",
		},
		{
			Required:    []string{"pack", "steps"},
			Description: "Workflow scenario",
		},
	}
}

func addScenarioExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "Scenario",
			"metadata": map[string]interface{}{
				"name": "test-scenario",
			},
			"spec": map[string]interface{}{
				"id":          "test-1",
				"task_type":   "general",
				"description": "Test scenario",
				"turns": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
			},
		},
	}
}
