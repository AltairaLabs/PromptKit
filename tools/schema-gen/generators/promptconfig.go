package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GeneratePromptConfigSchema generates the JSON Schema for PromptConfig configuration
func GeneratePromptConfigSchema() (interface{}, error) {
	return Generate(&SchemaConfig{
		Target:      &config.PromptConfigSchema{},
		Filename:    "promptconfig.json",
		Title:       "PromptKit Prompt Configuration",
		Description: "Prompt configuration for PromptKit",
		Customize:   addPromptConfigExample,
	})
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
