package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GeneratePersonaSchema generates the JSON Schema for Persona configuration
func GeneratePersonaSchema() (interface{}, error) {
	return Generate(&SchemaConfig{
		Target:      &config.PersonaConfigSchema{},
		Filename:    "persona.json",
		Title:       "PromptKit Persona Configuration",
		Description: "User persona configuration for self-play scenarios",
		Customize:   addPersonaExample,
	})
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
