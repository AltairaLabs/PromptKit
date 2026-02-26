// Package generators provides JSON schema generation functions for PromptKit configuration types.
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
	return Generate(&SchemaConfig{
		Target:      &config.ArenaConfig{},
		Filename:    "arena.json",
		Title:       "PromptArena Configuration",
		Description: "Main configuration for PromptArena test suites",
		Customize:   addArenaExample,
	})
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
