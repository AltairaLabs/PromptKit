package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateProviderSchema generates the JSON Schema for Provider configuration
func GenerateProviderSchema() (interface{}, error) {
	return Generate(&SchemaConfig{
		Target:      &config.ProviderConfig{},
		Filename:    "provider.json",
		Title:       "PromptArena Provider Configuration",
		Description: "Provider configuration for PromptArena LLM connections",
		Customize:   addProviderExample,
	})
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
