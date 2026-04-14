package generators

import (
	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateRuntimeConfigSchema generates the JSON Schema for RuntimeConfig.
func GenerateRuntimeConfigSchema() (interface{}, error) {
	//nolint:lll // description must be a single line
	desc := "Environment-specific configuration for running a pack: providers, MCP servers, state store, logging, evals, and hooks"
	return Generate(&SchemaConfig{
		Target:      &config.RuntimeConfig{},
		Filename:    "runtime-config.json",
		Title:       "PromptKit Runtime Configuration",
		Description: desc,
		Customize:   addRuntimeConfigExample,
	})
}

// allowSandboxAdditionalProps flips SandboxConfig's additionalProperties
// to true. SandboxConfig has a yaml-inline `Config map[string]any` that
// receives all mode-specific keys (image, network, mounts, ...), so the
// default strict setting would incorrectly reject valid configs.
func allowSandboxAdditionalProps(schema *jsonschema.Schema) {
	if schema.Definitions == nil {
		return
	}
	sb, ok := schema.Definitions["SandboxConfig"]
	if !ok {
		return
	}
	sb.AdditionalProperties = &jsonschema.Schema{}
}

func addRuntimeConfigExample(schema *jsonschema.Schema) {
	allowSandboxAdditionalProps(schema)
	schema.Examples = []interface{}{
		map[string]interface{}{
			"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
			"kind":       "RuntimeConfig",
			"metadata": map[string]interface{}{
				"name": "production",
			},
			"spec": runtimeConfigExampleSpec(),
		},
	}
}

func runtimeConfigExampleSpec() map[string]interface{} {
	return map[string]interface{}{
		"providers": []interface{}{
			map[string]interface{}{
				"id":    "main",
				"type":  "openai",
				"model": "gpt-4o",
				"credential": map[string]interface{}{
					"credential_env": "OPENAI_API_KEY",
				},
			},
		},
		"mcp_servers": []interface{}{
			map[string]interface{}{
				"name":    "filesystem",
				"command": "npx",
				"args":    []string{"-y", "@anthropic/mcp-filesystem"},
			},
		},
		"state_store": map[string]interface{}{
			"type": "redis",
			"redis": map[string]interface{}{
				"address": "localhost:6379",
			},
		},
		"logging": map[string]interface{}{
			"defaultLevel": "info",
			"format":       "json",
		},
	}
}
