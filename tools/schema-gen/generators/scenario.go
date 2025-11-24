package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateScenarioSchema generates the JSON Schema for Scenario configuration
func GenerateScenarioSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
		FieldNameTag:              "yaml",
	}

	schema := reflector.Reflect(&config.ScenarioConfig{})

	schema.ID = schemaBaseURL + "/scenario.json"
	schema.Title = "PromptArena Scenario Configuration"
	schema.Description = "Scenario configuration for PromptArena test cases"

	addScenarioExample(schema)

	return schema, nil
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
