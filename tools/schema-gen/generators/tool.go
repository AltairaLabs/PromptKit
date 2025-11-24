package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// GenerateToolSchema generates the JSON Schema for Tool configuration
func GenerateToolSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
	}

	schema := reflector.Reflect(&tools.ToolConfig{})

	schema.ID = schemaBaseURL + "/tool.json"
	schema.Title = "PromptKit Tool Configuration"
	schema.Description = "Tool/function configuration for PromptKit"

	addToolExample(schema)

	return schema, nil
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
