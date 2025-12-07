// Package main provides spec compliance tests for packc.
//
// These tests ensure packc produces output that conforms to the PromptPack spec
// at https://promptpack.org/docs/spec/schema-reference
//
// Key spec requirements tested:
// - Section 9: Tools must be defined at pack level with name, description, parameters
// - Section 7.1.7: Prompts reference tools by name (tools array)
// - Pack-level tools object contains full tool definitions
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpecCompliance_PackToolDefinitions tests that packc includes full tool definitions
// at the pack level per PromptPack spec Section 9.
//
// SPEC REQUIREMENT (Section 9):
// "tools" is an object where keys are tool names and values contain:
//   - name (required): Tool function name
//   - description (required): Tool description
//   - parameters (required): JSON Schema defining input parameters
func TestSpecCompliance_PackToolDefinitions(t *testing.T) {
	// Create temp directory for test files
	tmpDir := t.TempDir()

	// Create a tool definition file
	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: list_devices
spec:
  name: list_devices
  description: "List all IoT devices visible to the current customer"
  input_schema:
    type: object
    properties:
      customer_id:
        type: string
        description: "Customer ID"
    required:
      - customer_id
  output_schema:
    type: object
    properties:
      devices:
        type: array
  mode: mock
  timeout_ms: 1000
  mock_result: |
    {"devices": []}
`
	toolPath := filepath.Join(tmpDir, "tools", "list-devices.tool.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
	require.NoError(t, os.WriteFile(toolPath, []byte(toolYAML), 0644))

	// Create a prompt that references the tool
	promptYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test-prompt
spec:
  task_type: troubleshooter
  version: "1.0.0"
  description: "Test prompt with tool"
  template_engine:
    version: "1.0"
    syntax: "go-template"
    features: []
  system_template: |
    You are a helpful assistant.
  allowed_tools:
    - list_devices
  variables: []
`
	promptPath := filepath.Join(tmpDir, "prompts", "test.prompt.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(promptPath), 0755))
	require.NoError(t, os.WriteFile(promptPath, []byte(promptYAML), 0644))

	// Create arena config
	arenaYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: ArenaConfig
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: troubleshooter
      file: prompts/test.prompt.yaml
  tools:
    - file: tools/list-devices.tool.yaml
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
`
	arenaPath := filepath.Join(tmpDir, "arena.yaml")
	require.NoError(t, os.WriteFile(arenaPath, []byte(arenaYAML), 0644))

	// Load config
	cfg, err := config.LoadConfig(arenaPath)
	require.NoError(t, err, "Failed to load arena config")

	// Verify tools were loaded
	require.Len(t, cfg.LoadedTools, 1, "Expected 1 tool to be loaded")

	// Build memory repo and compile
	memRepo := buildMemoryRepo(cfg)
	registry := prompt.NewRegistryWithRepository(memRepo)
	require.NotNil(t, registry, "Registry should not be nil")

	compiler := prompt.NewPackCompiler(registry)

	// Parse tools from raw data
	parsedTools := parseToolsFromConfigData(t, cfg.LoadedTools)

	// Compile with tools from config
	pack, err := compiler.CompileFromRegistryWithParsedTools("test-pack", "packc-test", parsedTools)
	require.NoError(t, err, "Compilation should succeed")

	// === SPEC COMPLIANCE CHECKS ===

	// Check 1: Pack must have Tools field (Section 9)
	assert.NotNil(t, pack.Tools, "Pack must have Tools map (spec Section 9)")

	// Check 2: Tool must be in pack.Tools with the right name
	tool, exists := pack.Tools["list_devices"]
	assert.True(t, exists, "Tool 'list_devices' must be in pack.Tools")

	// Check 3: Tool must have required fields per spec
	if exists && tool != nil {
		assert.Equal(t, "list_devices", tool.Name, "Tool name must match")
		assert.NotEmpty(t, tool.Description, "Tool description is required (spec Section 9)")
		assert.NotNil(t, tool.Parameters, "Tool parameters are required (spec Section 9)")
	}

	// Check 4: Prompt.Tools should be []string of tool names (references)
	promptPack := pack.Prompts["troubleshooter"]
	require.NotNil(t, promptPack, "Prompt should exist in pack")
	assert.Contains(t, promptPack.Tools, "list_devices", "Prompt should reference tool by name")

	// Serialize and verify JSON structure
	data, err := json.MarshalIndent(pack, "", "  ")
	require.NoError(t, err, "Pack should serialize to JSON")

	// Parse back to verify structure
	var rawPack map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawPack))

	// Check JSON has tools at pack level
	toolsRaw, hasTools := rawPack["tools"]
	assert.True(t, hasTools, "JSON must have 'tools' field at pack level")

	if hasTools {
		toolsMap, ok := toolsRaw.(map[string]interface{})
		assert.True(t, ok, "tools must be an object/map")
		if ok {
			toolDef, hasListDevices := toolsMap["list_devices"]
			assert.True(t, hasListDevices, "tools must contain 'list_devices'")
			if hasListDevices {
				toolDefMap, ok := toolDef.(map[string]interface{})
				assert.True(t, ok, "tool definition must be an object")
				if ok {
					assert.Contains(t, toolDefMap, "name", "tool must have 'name'")
					assert.Contains(t, toolDefMap, "description", "tool must have 'description'")
					assert.Contains(t, toolDefMap, "parameters", "tool must have 'parameters'")
				}
			}
		}
	}
}

// TestSpecCompliance_ToolParametersAsJSONSchema tests that tool parameters
// are serialized as JSON Schema per spec Section 9.
func TestSpecCompliance_ToolParametersAsJSONSchema(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a tool with complex parameters
	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_sensor_data
spec:
  name: get_sensor_data
  description: "Get sensor readings from a device"
  input_schema:
    type: object
    properties:
      device_id:
        type: string
        description: "Device identifier"
      metric:
        type: string
        enum: ["temperature", "pressure", "vibration"]
        description: "Metric type to retrieve"
      window_hours:
        type: integer
        minimum: 1
        maximum: 168
        default: 24
        description: "Hours of history to retrieve"
    required:
      - device_id
      - metric
  output_schema:
    type: object
    properties:
      readings:
        type: array
  mode: mock
  mock_result: |
    {"readings": []}
`
	toolPath := filepath.Join(tmpDir, "tools", "sensor.tool.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
	require.NoError(t, os.WriteFile(toolPath, []byte(toolYAML), 0644))

	// Load the tool using tools.Registry (which handles YAML→JSON properly)
	registry := tools.NewRegistry()
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	require.NoError(t, registry.LoadToolFromBytes(toolPath, data))

	// Get the parsed tool
	toolsMap := registry.GetTools()
	require.Len(t, toolsMap, 1)

	toolDesc := toolsMap["get_sensor_data"]
	require.NotNil(t, toolDesc)

	// Create pack tool definition
	packTool := prompt.ConvertToolToPackTool(toolDesc.Name, toolDesc.Description, toolDesc.InputSchema)
	require.NotNil(t, packTool)

	// Verify parameters is valid JSON Schema
	assert.NotNil(t, packTool.Parameters, "Parameters must not be nil")

	// Check parameters structure
	params, ok := packTool.Parameters.(map[string]interface{})
	require.True(t, ok, "Parameters should be a map")

	assert.Equal(t, "object", params["type"], "Parameters should have type: object")
	props, hasProps := params["properties"]
	assert.True(t, hasProps, "Parameters should have properties")

	if propsMap, ok := props.(map[string]interface{}); ok {
		deviceID, hasDeviceID := propsMap["device_id"]
		assert.True(t, hasDeviceID, "Should have device_id property")
		if didMap, ok := deviceID.(map[string]interface{}); ok {
			assert.Equal(t, "string", didMap["type"])
		}

		metric, hasMetric := propsMap["metric"]
		assert.True(t, hasMetric, "Should have metric property")
		if metricMap, ok := metric.(map[string]interface{}); ok {
			assert.Equal(t, "string", metricMap["type"])
			// Enum should be preserved
			enumVal, hasEnum := metricMap["enum"]
			assert.True(t, hasEnum, "metric should have enum constraint")
			if enumSlice, ok := enumVal.([]interface{}); ok {
				assert.Len(t, enumSlice, 3, "enum should have 3 values")
			}
		}
	}

	// Check required array
	required, hasRequired := params["required"]
	assert.True(t, hasRequired, "Parameters should have required array")
	if reqSlice, ok := required.([]interface{}); ok {
		assert.Contains(t, reqSlice, "device_id")
		assert.Contains(t, reqSlice, "metric")
	}
}

// TestSpecCompliance_MultipleTools tests pack with multiple tools
func TestSpecCompliance_MultipleTools(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple tool files
	toolsList := []struct {
		name    string
		content string
	}{
		{
			name: "list_devices",
			content: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: list_devices
spec:
  name: list_devices
  description: "List all devices"
  input_schema:
    type: object
    properties:
      filter:
        type: string
    required: []
  output_schema:
    type: object
    properties:
      devices:
        type: array
  mode: mock
  mock_result: '{"devices": []}'
`,
		},
		{
			name: "get_logs",
			content: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_logs
spec:
  name: get_logs
  description: "Get device logs"
  input_schema:
    type: object
    properties:
      device_id:
        type: string
      limit:
        type: integer
    required:
      - device_id
  output_schema:
    type: object
    properties:
      logs:
        type: array
  mode: mock
  mock_result: '{"logs": []}'
`,
		},
	}

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tools"), 0755))

	var toolRefs []config.ToolRef
	var loadedTools []config.ToolData

	for _, tool := range toolsList {
		toolPath := filepath.Join(tmpDir, "tools", tool.name+".tool.yaml")
		require.NoError(t, os.WriteFile(toolPath, []byte(tool.content), 0644))

		toolRefs = append(toolRefs, config.ToolRef{File: "tools/" + tool.name + ".tool.yaml"})
		loadedTools = append(loadedTools, config.ToolData{
			FilePath: toolPath,
			Data:     []byte(tool.content),
		})
	}

	// Create prompt that uses both tools
	promptYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test-prompt
spec:
  task_type: multi-tool-test
  version: "1.0.0"
  description: "Test with multiple tools"
  template_engine:
    version: "1.0"
    syntax: "go-template"
    features: []
  system_template: "You can use list_devices and get_logs."
  allowed_tools:
    - list_devices
    - get_logs
  variables: []
`
	promptPath := filepath.Join(tmpDir, "prompts", "test.prompt.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(promptPath), 0755))
	require.NoError(t, os.WriteFile(promptPath, []byte(promptYAML), 0644))

	// Create arena config
	arenaYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: ArenaConfig
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: multi-tool-test
      file: prompts/test.prompt.yaml
  tools:
    - file: tools/list_devices.tool.yaml
    - file: tools/get_logs.tool.yaml
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
`
	arenaPath := filepath.Join(tmpDir, "arena.yaml")
	require.NoError(t, os.WriteFile(arenaPath, []byte(arenaYAML), 0644))

	// Load and compile
	cfg, err := config.LoadConfig(arenaPath)
	require.NoError(t, err)

	memRepo := buildMemoryRepo(cfg)
	registry := prompt.NewRegistryWithRepository(memRepo)
	compiler := prompt.NewPackCompiler(registry)

	parsedTools := parseToolsFromConfigData(t, cfg.LoadedTools)
	pack, err := compiler.CompileFromRegistryWithParsedTools("multi-tool-pack", "packc-test", parsedTools)
	require.NoError(t, err)

	// Verify all tools are in pack
	assert.Len(t, pack.Tools, 2, "Pack should have 2 tools")
	assert.Contains(t, pack.Tools, "list_devices")
	assert.Contains(t, pack.Tools, "get_logs")

	// Verify each tool has required fields
	for name, tool := range pack.Tools {
		assert.NotEmpty(t, tool.Name, "Tool %s must have name", name)
		assert.NotEmpty(t, tool.Description, "Tool %s must have description", name)
		assert.NotNil(t, tool.Parameters, "Tool %s must have parameters", name)
	}
}

// TestSpecCompliance_SDKPackLoadable tests that compiled pack can be loaded by SDK
func TestSpecCompliance_SDKPackLoadable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal tool
	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: simple_tool
spec:
  name: simple_tool
  description: "A simple test tool"
  input_schema:
    type: object
    properties:
      input:
        type: string
    required: []
  output_schema:
    type: object
    properties:
      result:
        type: string
  mode: mock
  mock_result: '{"result": "ok"}'
`
	toolPath := filepath.Join(tmpDir, "tools", "simple.tool.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
	require.NoError(t, os.WriteFile(toolPath, []byte(toolYAML), 0644))

	// Create prompt using the tool
	promptYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: sdk-test
spec:
  task_type: sdk-loader-test
  version: "1.0.0"
  description: "Test SDK loading"
  template_engine:
    version: "1.0"
    syntax: "go-template"
    features: []
  system_template: "Test prompt"
  allowed_tools:
    - simple_tool
  variables: []
`
	promptPath := filepath.Join(tmpDir, "prompts", "sdk.prompt.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(promptPath), 0755))
	require.NoError(t, os.WriteFile(promptPath, []byte(promptYAML), 0644))

	arenaYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: ArenaConfig
metadata:
  name: sdk-test-arena
spec:
  prompt_configs:
    - id: sdk-loader-test
      file: prompts/sdk.prompt.yaml
  tools:
    - file: tools/simple.tool.yaml
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
`
	arenaPath := filepath.Join(tmpDir, "arena.yaml")
	require.NoError(t, os.WriteFile(arenaPath, []byte(arenaYAML), 0644))

	// Load and compile
	cfg, err := config.LoadConfig(arenaPath)
	require.NoError(t, err)

	memRepo := buildMemoryRepo(cfg)
	registry := prompt.NewRegistryWithRepository(memRepo)
	compiler := prompt.NewPackCompiler(registry)

	parsedTools := parseToolsFromConfigData(t, cfg.LoadedTools)
	pack, err := compiler.CompileFromRegistryWithParsedTools("sdk-test-pack", "packc-test", parsedTools)
	require.NoError(t, err)

	// Write pack to file
	packPath := filepath.Join(tmpDir, "test.pack.json")
	data, err := json.MarshalIndent(pack, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(packPath, data, 0644))

	// Now verify it can be loaded by simulating SDK's validation
	// (We can't import sdk here due to circular deps, so we validate the JSON structure)
	var loadedPack map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &loadedPack))

	// SDK validation checks
	assert.NotEmpty(t, loadedPack["id"], "Pack must have id")
	assert.NotEmpty(t, loadedPack["name"], "Pack must have name")
	assert.NotEmpty(t, loadedPack["version"], "Pack must have version")

	prompts, ok := loadedPack["prompts"].(map[string]interface{})
	require.True(t, ok, "Pack must have prompts map")

	promptDef, ok := prompts["sdk-loader-test"].(map[string]interface{})
	require.True(t, ok, "Pack must have sdk-loader-test prompt")

	// SDK validates that prompt.tools references exist in pack.tools
	toolsInPrompt, ok := promptDef["tools"].([]interface{})
	require.True(t, ok, "Prompt must have tools array")
	assert.Contains(t, toolsInPrompt, "simple_tool")

	// Pack must have tools map with tool definition
	toolsMap, ok := loadedPack["tools"].(map[string]interface{})
	require.True(t, ok, "Pack must have tools map")
	_, toolExists := toolsMap["simple_tool"]
	assert.True(t, toolExists, "Pack.tools must contain 'simple_tool' referenced by prompt")
}

// parseToolsFromConfigData parses raw tool YAML data into ParsedTool structs
// Uses the tools.Registry which handles YAML→JSON conversion properly
func parseToolsFromConfigData(t *testing.T, configTools []config.ToolData) []prompt.ParsedTool {
	t.Helper()
	var result []prompt.ParsedTool

	// Create a temporary registry to parse tools
	registry := tools.NewRegistry()

	for _, td := range configTools {
		// Use registry's LoadToolFromBytes which handles YAML→JSON properly
		err := registry.LoadToolFromBytes(td.FilePath, td.Data)
		if err != nil {
			// Skip non-tool files or files with errors
			t.Logf("Skipping %s: %v", td.FilePath, err)
			continue
		}
	}

	// Extract parsed tools from registry using GetTools()
	for name, tool := range registry.GetTools() {
		result = append(result, prompt.ParsedTool{
			Name:        name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	return result
}
