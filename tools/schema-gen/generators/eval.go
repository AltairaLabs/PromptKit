package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateEvalSchema generates the JSON Schema for Eval configuration
func GenerateEvalSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
		FieldNameTag:              "yaml",
	}

	schema := reflector.Reflect(&config.EvalConfig{})

	schema.Version = "https://json-schema.org/draft-07/schema"
	schema.ID = schemaBaseURL + "/eval.json"
	schema.Title = "PromptArena Eval Configuration"
	schema.Description = "Eval configuration for replaying and validating saved conversations"

	// Allow the standard $schema field
	allowSchemaField(schema)

	addEvalExample(schema)

	return schema, nil
}

func addEvalExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "Eval",
			"metadata": map[string]interface{}{
				"name": "my-eval",
			},
			"spec": map[string]interface{}{
				"id":          "eval-saved-conversation",
				"description": "Evaluate saved customer support conversation",
				"recording": map[string]interface{}{
					"path": "recordings/session-2024-01-15.recording.json",
					"type": "session",
				},
				"judge_targets": map[string]interface{}{
					"default": map[string]interface{}{
						"type":  "openai",
						"model": "gpt-4o",
						"id":    "gpt-4o-judge",
					},
				},
				"assertions": []interface{}{
					map[string]interface{}{
						"type": "llm_judge",
						"config": map[string]interface{}{
							"judge":    "default",
							"criteria": "Was the customer issue resolved satisfactorily?",
							"expected": "pass",
						},
					},
				},
				"tags": []string{"customer-support", "evaluation"},
				"mode": "instant",
			},
		},
	}
}
