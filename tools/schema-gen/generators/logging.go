package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateLoggingSchema generates the JSON Schema for LoggingConfig configuration
func GenerateLoggingSchema() (interface{}, error) {
	return Generate(&SchemaConfig{
		Target:      &config.LoggingConfig{},
		Filename:    "logging.json",
		Title:       "PromptKit Logging Configuration",
		Description: "Configuration for structured logging with per-module log levels",
		Customize:   addLoggingExample,
	})
}

func addLoggingExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "LoggingConfig",
			"metadata": map[string]interface{}{
				"name": "development",
			},
			"spec": map[string]interface{}{
				"defaultLevel": "info",
				"format":       "text",
				"commonFields": map[string]interface{}{
					"environment": "development",
					"service":     "promptkit",
				},
				"modules": []interface{}{
					map[string]interface{}{
						"name":  "runtime.pipeline",
						"level": "debug",
					},
					map[string]interface{}{
						"name":  "providers.openai",
						"level": "debug",
					},
				},
			},
		},
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "LoggingConfig",
			"metadata": map[string]interface{}{
				"name": "production",
			},
			"spec": map[string]interface{}{
				"defaultLevel": "warn",
				"format":       "json",
				"commonFields": map[string]interface{}{
					"environment": "production",
					"service":     "promptkit",
					"cluster":     "us-east-1",
				},
				"modules": []interface{}{
					map[string]interface{}{
						"name":  "runtime",
						"level": "info",
					},
				},
			},
		},
	}
}
