package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

const (
	schemaBaseURL            = "https://promptkit.altairalabs.ai/schemas/v1alpha1"
	defaultTemperature       = 0.7
	defaultMaxTokens         = 1000
	defaultProviderMaxTokens = 2000
	defaultConcurrency       = 1
)

// GenerateArenaSchema generates the JSON Schema for Arena configuration
func GenerateArenaSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		ExpandedStruct:             true,
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: false, // Use omitempty to determine required fields
	}

	schema := reflector.Reflect(&config.ArenaConfig{})

	schema.ID = schemaBaseURL + "/arena.json"
	schema.Title = "PromptArena Configuration"
	schema.Description = "Main configuration for PromptArena test suites"

	// Allow the standard $schema field
	allowSchemaField(schema)

	// Add example
	addArenaExample(schema)

	return schema, nil
}

func addArenaExample(schema *jsonschema.Schema) {
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "Arena",
			"metadata": map[string]interface{}{
				"name": "my-test-suite",
			},
			"spec": map[string]interface{}{
				"providers": []interface{}{
					map[string]interface{}{
						"file": "providers/openai.yaml",
					},
				},
				"scenarios": []interface{}{
					map[string]interface{}{
						"file": "scenarios/test-scenario.yaml",
					},
				},
				"defaults": map[string]interface{}{
					"temperature": defaultTemperature,
					"max_tokens":  defaultMaxTokens,
					"concurrency": defaultConcurrency,
					"output": map[string]interface{}{
						"dir":     "out",
						"formats": []string{"json", "html"},
					},
				},
			},
		},
	}
}
