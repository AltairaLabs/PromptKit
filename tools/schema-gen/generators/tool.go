package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateToolSchema generates the JSON Schema for Tool configuration
func GenerateToolSchema() (interface{}, error) {
	return Generate(&SchemaConfig{
		Target:      &config.ToolConfigSchema{},
		Filename:    "tool.json",
		Title:       "PromptKit Tool Configuration",
		Description: "Tool/function configuration for PromptKit",
		Customize:   addToolExample,
	})
}

func addToolExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "Tool",
			"metadata": map[string]interface{}{
				"name": "weather-tool",
			},
			"spec": map[string]interface{}{
				"name":        "get_weather",
				"description": "Get current weather",
				"mode":        "mock",
			},
		},
	}
}
