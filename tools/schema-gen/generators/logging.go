package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateLoggingSchema generates the JSON Schema for LoggingConfig configuration
func GenerateLoggingSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
		FieldNameTag:              "yaml",
	}

	schema := reflector.Reflect(&config.LoggingConfig{})

	schema.Version = "https://json-schema.org/draft-07/schema"
	schema.ID = schemaBaseURL + "/logging.json"
	schema.Title = "PromptKit Logging Configuration"
	schema.Description = "Configuration for structured logging with per-module log levels"

	// Allow the standard $schema field
	allowSchemaField(schema)

	addLoggingExample(schema)

	return schema, nil
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
