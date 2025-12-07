package prompt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestRegistryWithPrompts creates a registry with a prompt pre-loaded for testing
func createTestRegistryWithPrompts() *Registry {
	repo := newMockRepository()
	// Add a test prompt to the repository
	repo.prompts["test-activity"] = &Config{
		Spec: Spec{
			TaskType:       "test-activity",
			SystemTemplate: "Hello {{name}}",
			Variables: []VariableMetadata{
				{Name: "name", Required: true},
			},
		},
	}
	return NewRegistryWithRepository(repo)
}

// TestConvertToolToPackTool tests the ConvertToolToPackTool helper function
func TestConvertToolToPackTool(t *testing.T) {
	t.Run("converts tool with all fields", func(t *testing.T) {
		inputSchema := json.RawMessage(`{"type":"object","properties":{"device_id":{"type":"string"}}}`)

		result := ConvertToolToPackTool("list_devices", "List all devices", inputSchema)

		require.NotNil(t, result)
		assert.Equal(t, "list_devices", result.Name)
		assert.Equal(t, "List all devices", result.Description)
		assert.NotNil(t, result.Parameters)

		// Verify parameters is a map with the expected structure
		params, ok := result.Parameters.(map[string]interface{})
		require.True(t, ok, "Parameters should be a map")
		assert.Equal(t, "object", params["type"])
	})

	t.Run("converts tool with empty input schema", func(t *testing.T) {
		result := ConvertToolToPackTool("simple_tool", "A simple tool", nil)

		require.NotNil(t, result)
		assert.Equal(t, "simple_tool", result.Name)
		assert.Equal(t, "A simple tool", result.Description)
		assert.Nil(t, result.Parameters)
	})

	t.Run("handles invalid JSON in input schema gracefully", func(t *testing.T) {
		invalidSchema := json.RawMessage(`{invalid json}`)

		result := ConvertToolToPackTool("broken_tool", "Tool with broken schema", invalidSchema)

		require.NotNil(t, result)
		assert.Equal(t, "broken_tool", result.Name)
		// Parameters should be nil when JSON parsing fails
		assert.Nil(t, result.Parameters)
	})
}

// TestCompileFromRegistryWithParsedTools tests tool compilation with pre-parsed tools
func TestCompileFromRegistryWithParsedTools(t *testing.T) {
	registry := createTestRegistryWithPrompts()
	compiler := NewPackCompiler(registry)

	t.Run("compiles pack with parsed tools", func(t *testing.T) {
		parsedTools := []ParsedTool{
			{
				Name:        "tool1",
				Description: "First tool",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
			{
				Name:        "tool2",
				Description: "Second tool",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
			},
		}

		pack, err := compiler.CompileFromRegistryWithParsedTools("test-pack", "packc v1.0.0", parsedTools)

		require.NoError(t, err)
		require.NotNil(t, pack)
		assert.Equal(t, "test-pack", pack.ID)

		// Verify tools are included
		require.NotNil(t, pack.Tools)
		assert.Len(t, pack.Tools, 2)

		// Check first tool
		tool1, exists := pack.Tools["tool1"]
		require.True(t, exists)
		assert.Equal(t, "tool1", tool1.Name)
		assert.Equal(t, "First tool", tool1.Description)

		// Check second tool
		tool2, exists := pack.Tools["tool2"]
		require.True(t, exists)
		assert.Equal(t, "tool2", tool2.Name)
		assert.Equal(t, "Second tool", tool2.Description)
	})

	t.Run("compiles pack with empty tools list", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistryWithParsedTools("test-pack", "packc v1.0.0", nil)

		require.NoError(t, err)
		require.NotNil(t, pack)
		// Tools map is initialized but empty when no tools provided
		assert.Empty(t, pack.Tools)
	})

	t.Run("compiles pack with zero-length tools slice", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistryWithParsedTools("test-pack", "packc v1.0.0", []ParsedTool{})

		require.NoError(t, err)
		require.NotNil(t, pack)
		assert.Empty(t, pack.Tools)
	})
}

// TestCompileFromRegistryWithTools tests tool compilation from raw data
// Note: parseToolData only supports JSON, not YAML
func TestCompileFromRegistryWithTools(t *testing.T) {
	registry := createTestRegistryWithPrompts()
	compiler := NewPackCompiler(registry)

	t.Run("compiles pack with JSON tool data", func(t *testing.T) {
		// Use JSON format since parseYAMLConfig only handles JSON
		toolJSON := `{"kind":"Tool","metadata":{"name":"test_tool"},"spec":{"name":"test_tool","description":"A test tool","input_schema":{"type":"object"}}}`
		toolData := []ToolData{
			{
				FilePath: "test_tool.json",
				Data:     []byte(toolJSON),
			},
		}

		pack, err := compiler.CompileFromRegistryWithTools("test-pack", "packc v1.0.0", toolData)

		require.NoError(t, err)
		require.NotNil(t, pack)

		// Verify tool was parsed and added
		require.NotNil(t, pack.Tools)
		tool, exists := pack.Tools["test_tool"]
		require.True(t, exists, "Tool should exist in pack")
		assert.Equal(t, "test_tool", tool.Name)
		assert.Equal(t, "A test tool", tool.Description)
	})

	t.Run("compiles pack with empty tool data", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistryWithTools("test-pack", "packc v1.0.0", nil)

		require.NoError(t, err)
		require.NotNil(t, pack)
		assert.Empty(t, pack.Tools)
	})

	t.Run("handles non-JSON data gracefully", func(t *testing.T) {
		// YAML is not supported by parseToolData
		yamlData := `kind: Tool
metadata:
  name: test
`
		toolData := []ToolData{
			{
				FilePath: "invalid.yaml",
				Data:     []byte(yamlData),
			},
		}

		_, err := compiler.CompileFromRegistryWithTools("test-pack", "packc v1.0.0", toolData)

		// Should return an error for non-JSON data
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse tool")
	})

	t.Run("skips non-tool JSON files", func(t *testing.T) {
		// JSON for a different kind (not a Tool)
		promptJSON := `{"kind":"PromptConfig","metadata":{"name":"test_prompt"},"spec":{"task_type":"testing"}}`
		toolData := []ToolData{
			{
				FilePath: "prompt.json",
				Data:     []byte(promptJSON),
			},
		}

		pack, err := compiler.CompileFromRegistryWithTools("test-pack", "packc v1.0.0", toolData)

		require.NoError(t, err)
		require.NotNil(t, pack)
		// No tools should be added since file is not a Tool (map is empty)
		assert.Empty(t, pack.Tools)
	})
}

// TestParseToolData tests the parseToolData helper function
func TestParseToolData(t *testing.T) {
	t.Run("parses valid tool JSON", func(t *testing.T) {
		toolJSON := []byte(`{"kind":"Tool","metadata":{"name":"my_tool"},"spec":{"name":"my_tool","description":"My test tool","input_schema":{"type":"object","properties":{"param1":{"type":"string"}}}}}`)

		tool, err := parseToolData(toolJSON)

		require.NoError(t, err)
		require.NotNil(t, tool)
		assert.Equal(t, "my_tool", tool.Name)
		assert.Equal(t, "My test tool", tool.Description)
		assert.NotNil(t, tool.Parameters)
	})

	t.Run("returns nil for non-tool JSON", func(t *testing.T) {
		nonToolJSON := []byte(`{"kind":"PromptConfig","metadata":{"name":"prompt"},"spec":{"task_type":"test"}}`)

		tool, err := parseToolData(nonToolJSON)

		require.NoError(t, err)
		assert.Nil(t, tool, "Should return nil for non-tool JSON")
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		invalidJSON := []byte(`{invalid json content`)

		tool, err := parseToolData(invalidJSON)

		require.Error(t, err)
		assert.Nil(t, tool)
	})

	t.Run("handles tool without input_schema", func(t *testing.T) {
		toolJSON := []byte(`{"kind":"Tool","metadata":{"name":"simple_tool"},"spec":{"name":"simple_tool","description":"A tool without input schema"}}`)

		tool, err := parseToolData(toolJSON)

		require.NoError(t, err)
		require.NotNil(t, tool)
		assert.Equal(t, "simple_tool", tool.Name)
		assert.Equal(t, "A tool without input schema", tool.Description)
	})
}

// TestParseYAMLConfig tests the parseYAMLConfig helper function
func TestParseYAMLConfig(t *testing.T) {
	t.Run("parses valid JSON config", func(t *testing.T) {
		// parseYAMLConfig actually only handles JSON (YAML parsing is external)
		jsonData := []byte(`{"kind":"Tool","metadata":{"name":"test"},"spec":{"name":"test"}}`)

		var config struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}

		err := parseYAMLConfig(jsonData, &config)

		require.NoError(t, err)
		assert.Equal(t, "Tool", config.Kind)
		assert.Equal(t, "test", config.Metadata.Name)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		invalidData := []byte(`{{invalid}}`)

		var config map[string]interface{}
		err := parseYAMLConfig(invalidData, &config)

		// parseYAMLConfig returns error for non-JSON data
		require.Error(t, err)
	})

	t.Run("parses JSON with different kinds", func(t *testing.T) {
		promptJSON := []byte(`{"kind":"PromptConfig","metadata":{"name":"my_prompt"},"spec":{"task_type":"testing"}}`)

		var config struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}

		err := parseYAMLConfig(promptJSON, &config)

		require.NoError(t, err)
		assert.Equal(t, "PromptConfig", config.Kind)
		assert.Equal(t, "my_prompt", config.Metadata.Name)
	})
}

// TestPackToolsInJSON tests that tools are properly serialized in pack JSON
func TestPackToolsInJSON(t *testing.T) {
	t.Run("tools serialize correctly to JSON", func(t *testing.T) {
		pack := &Pack{
			ID:      "test-pack",
			Version: "v1.0.0",
			Tools: map[string]*PackTool{
				"my_tool": {
					Name:        "my_tool",
					Description: "A tool for testing",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
			Prompts: map[string]*PackPrompt{
				"test": {
					ID:             "test",
					SystemTemplate: "Hello",
					Version:        "1.0.0",
				},
			},
		}

		data, err := json.Marshal(pack)
		require.NoError(t, err)

		// Unmarshal and verify
		var unmarshaled Pack
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		require.NotNil(t, unmarshaled.Tools)
		tool, exists := unmarshaled.Tools["my_tool"]
		require.True(t, exists)
		assert.Equal(t, "my_tool", tool.Name)
		assert.Equal(t, "A tool for testing", tool.Description)
		assert.NotNil(t, tool.Parameters)
	})
}
