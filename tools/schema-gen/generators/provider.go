package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateProviderSchema generates the JSON Schema for Provider configuration
func GenerateProviderSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
		FieldNameTag:              "yaml",
	}

	schema := reflector.Reflect(&config.ProviderConfig{})

	schema.Version = "https://json-schema.org/draft-07/schema"
	schema.ID = schemaBaseURL + "/provider.json"
	schema.Title = "PromptArena Provider Configuration"
	schema.Description = "Provider configuration for PromptArena LLM connections"

	// Allow the standard $schema field
	allowSchemaField(schema)

	addProviderExample(schema)

	return schema, nil
}

func addProviderExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "Provider",
			"metadata": map[string]interface{}{
				"name": "openai-gpt4",
			},
			"spec": map[string]interface{}{
				"id":    "gpt4",
				"type":  "openai",
				"model": "gpt-4",
				"defaults": map[string]interface{}{
					"temperature": defaultTemperature,
					"max_tokens":  defaultProviderMaxTokens,
				},
			},
		},
	}
}
