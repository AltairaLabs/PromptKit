package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GeneratePersonaSchema generates the JSON Schema for Persona configuration
func GeneratePersonaSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
		FieldNameTag:              "yaml",
	}

	schema := reflector.Reflect(&config.PersonaConfigSchema{})

	schema.ID = schemaBaseURL + "/persona.json"
	schema.Title = "PromptKit Persona Configuration"
	schema.Description = "User persona configuration for self-play scenarios"

	addPersonaExample(schema)

	return schema, nil
}

func addPersonaExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "Persona",
			"metadata": map[string]interface{}{
				"name": "customer-persona",
			},
			"spec": map[string]interface{}{
				"id":       "customer",
				"provider": "gpt4",
			},
		},
	}
}
