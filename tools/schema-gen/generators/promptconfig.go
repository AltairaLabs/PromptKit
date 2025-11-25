package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GeneratePromptConfigSchema generates the JSON Schema for PromptConfig configuration
func GeneratePromptConfigSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
		FieldNameTag:              "yaml",
	}

	schema := reflector.Reflect(&config.PromptConfigSchema{})

	schema.Version = "https://json-schema.org/draft-07/schema"
	schema.ID = schemaBaseURL + "/promptconfig.json"
	schema.Title = "PromptKit Prompt Configuration"
	schema.Description = "Prompt configuration for PromptKit"

	// Allow the standard $schema field
	allowSchemaField(schema)

	addPromptConfigExample(schema)

	return schema, nil
}

func addPromptConfigExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "PromptConfig",
			"metadata": map[string]interface{}{
				"name": "customer-support",
			},
			"spec": map[string]interface{}{
				"task_type":       "support",
				"version":         "1.0.0",
				"description":     "Customer support assistant",
				"system_template": "You are a helpful customer support assistant.",
			},
		},
	}
}
