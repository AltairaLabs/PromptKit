package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TestNewRegistry verifies registry initialization
func TestNewRegistry(t *testing.T) {
	registry := tools.NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}

	// Verify default executors are registered
	list := registry.List()
	if len(list) != 0 {
		t.Errorf("Expected empty registry, got %d tools", len(list))
	}
}

// TestRegister verifies tool registration
func TestRegister(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "test_tool",
		Description:  "A test tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"input": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"output": {"type": "string"}}}`),
		Mode:         "mock",
		TimeoutMs:    1000,
	}

	err := registry.Register(descriptor)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify tool is registered
	retrieved := registry.Get("test_tool")
	if retrieved == nil {
		t.Fatal("Failed to retrieve registered tool")
	}

	if retrieved.Name != "test_tool" {
		t.Errorf("Expected name 'test_tool', got '%s'", retrieved.Name)
	}
}

// TestGet verifies tool retrieval
func TestGet(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "my_tool",
		Description:  "Test",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:         "mock",
		TimeoutMs:    500,
	}

	_ = registry.Register(descriptor)

	// Test successful retrieval
	tool := registry.Get("my_tool")
	if tool == nil {
		t.Fatal("Get returned nil for existing tool")
	}

	if tool.Name != "my_tool" {
		t.Errorf("Expected 'my_tool', got '%s'", tool.Name)
	}

	// Test retrieval of non-existent tool
	notFound := registry.Get("nonexistent")
	if notFound != nil {
		t.Error("Get should return nil for non-existent tool")
	}
}

// TestList verifies listing all tool names
func TestList(t *testing.T) {
	registry := tools.NewRegistry()

	// Empty registry
	list := registry.List()
	if len(list) != 0 {
		t.Errorf("Expected 0 tools, got %d", len(list))
	}

	// Add tools
	toolNames := []string{"tool1", "tool2", "tool3"}
	for _, name := range toolNames {
		descriptor := &tools.ToolDescriptor{
			Name:         name,
			Description:  "Test",
			InputSchema:  json.RawMessage(`{"type": "object"}`),
			OutputSchema: json.RawMessage(`{"type": "object"}`),
			Mode:         "mock",
			TimeoutMs:    1000,
		}
		_ = registry.Register(descriptor)
	}

	list = registry.List()
	if len(list) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(list))
	}

	// Verify all tools are present
	toolMap := make(map[string]bool)
	for _, name := range list {
		toolMap[name] = true
	}

	for _, name := range toolNames {
		if !toolMap[name] {
			t.Errorf("Tool '%s' not found in list", name)
		}
	}
}

// NOTE: The following test functions were removed as part of legacy code elimination:
// - TestLoadFromFile, TestLoadFromFile_InvalidJSON, TestLoadFromFile_NonExistent
// - TestLoadTool_JSON, TestLoadTool_YAML, TestLoadTool_YML, TestLoadTool_InvalidDescriptor
// - TestLoadToolsFromDirectory, TestLoadToolsFromDirectory_NonExistent
//
// These tests verified file loading methods (LoadFromFile, LoadTool, LoadToolsFromDirectory)
// that have been removed from the Registry. The runtime now uses the repository pattern where
// all file I/O happens in the config layer, and pre-loaded tools are passed to the runtime via
// ToolRepository implementations. LoadToolFromBytes remains for backward compatibility with
// pre-read file data.

// TestGetTool verifies GetTool with error returns
func TestGetTool(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "get_test",
		Description:  "Test",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:         "mock",
		TimeoutMs:    1000,
	}

	_ = registry.Register(descriptor)

	// Test successful retrieval
	tool, err := registry.GetTool("get_test")
	if err != nil {
		t.Fatalf("GetTool failed: %v", err)
	}

	if tool.Name != "get_test" {
		t.Errorf("Expected 'get_test', got '%s'", tool.Name)
	}

	// Test non-existent tool
	_, err = registry.GetTool("nonexistent")
	if err == nil {
		t.Error("GetTool should return error for non-existent tool")
	}
}

// TestGetTools verifies getting all tool descriptors
func TestGetTools(t *testing.T) {
	registry := tools.NewRegistry()

	// Add multiple tools
	for i := 1; i <= 3; i++ {
		descriptor := &tools.ToolDescriptor{
			Name:         "tool" + string(rune('0'+i)),
			Description:  "Test",
			InputSchema:  json.RawMessage(`{"type": "object"}`),
			OutputSchema: json.RawMessage(`{"type": "object"}`),
			Mode:         "mock",
			TimeoutMs:    1000,
		}
		_ = registry.Register(descriptor)
	}

	allTools := registry.GetTools()
	if len(allTools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(allTools))
	}

	// Verify it's a copy (modifications don't affect registry)
	allTools["new_tool"] = &tools.ToolDescriptor{Name: "new_tool"}

	if registry.Get("new_tool") != nil {
		t.Error("GetTools should return a copy, not direct reference")
	}
}

// TestGetToolsByNames verifies getting specific tools by name
func TestGetToolsByNames(t *testing.T) {
	registry := tools.NewRegistry()

	// Register tools
	names := []string{"tool_a", "tool_b", "tool_c"}
	for _, name := range names {
		descriptor := &tools.ToolDescriptor{
			Name:         name,
			Description:  "Test",
			InputSchema:  json.RawMessage(`{"type": "object"}`),
			OutputSchema: json.RawMessage(`{"type": "object"}`),
			Mode:         "mock",
			TimeoutMs:    1000,
		}
		_ = registry.Register(descriptor)
	}

	// Test successful retrieval
	tools, err := registry.GetToolsByNames([]string{"tool_a", "tool_c"})
	if err != nil {
		t.Fatalf("GetToolsByNames failed: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	// Test with non-existent tool
	_, err = registry.GetToolsByNames([]string{"tool_a", "nonexistent"})
	if err == nil {
		t.Error("GetToolsByNames should fail if any tool doesn't exist")
	}
}

// TestRegisterExecutor verifies executor registration
func TestRegisterExecutor(t *testing.T) {
	registry := tools.NewRegistry()

	// Default executors should be registered
	staticExec := tools.NewMockStaticExecutor()
	scriptedExec := tools.NewMockScriptedExecutor()

	// Re-register should not cause errors
	registry.RegisterExecutor(staticExec)
	registry.RegisterExecutor(scriptedExec)

	// Registry should still function
	if registry.List() == nil {
		t.Error("Registry corrupted after RegisterExecutor")
	}
}

// TestExecute_MockStatic verifies execution with static mock
func TestExecute_MockStatic(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "static_tool",
		Description:  "Static mock tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"input": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"output": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"output": "static response"}`),
		TimeoutMs:    1000,
	}

	_ = registry.Register(descriptor)

	args := json.RawMessage(`{"input": "test"}`)
	result, err := registry.Execute("static_tool", args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Name != "static_tool" {
		t.Errorf("Expected tool name 'static_tool', got '%s'", result.Name)
	}

	if result.Error != "" {
		t.Errorf("Expected no error, got '%s'", result.Error)
	}

	if result.LatencyMs < 0 {
		t.Errorf("Expected non-negative latency, got %d", result.LatencyMs)
	}

	// Verify result content
	var resultData map[string]interface{}
	if err := json.Unmarshal(result.Result, &resultData); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if resultData["output"] != "static response" {
		t.Errorf("Expected 'static response', got '%v'", resultData["output"])
	}
}

// TestExecute_MockScripted verifies execution with scripted mock
func TestExecute_MockScripted(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "scripted_tool",
		Description:  "Scripted mock tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"greeting": {"type": "string"}}}`),
		Mode:         "mock",
		MockTemplate: `{"greeting": "Hello {{ .name }}"}`,
		TimeoutMs:    1000,
	}

	_ = registry.Register(descriptor)

	args := json.RawMessage(`{"name": "World"}`)
	result, err := registry.Execute("scripted_tool", args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Error != "" {
		t.Errorf("Expected no error, got '%s'", result.Error)
	}

	// Verify templating worked
	var resultData map[string]interface{}
	if err := json.Unmarshal(result.Result, &resultData); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if resultData["greeting"] != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%v'", resultData["greeting"])
	}
}

// TestExecute_NonExistentTool verifies error handling
func TestExecute_NonExistentTool(t *testing.T) {
	registry := tools.NewRegistry()

	args := json.RawMessage(`{}`)
	_, err := registry.Execute("nonexistent", args)
	if err == nil {
		t.Error("Execute should fail for non-existent tool")
	}
}

// TestExecute_InvalidArgs verifies argument validation
func TestExecute_InvalidArgs(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:        "validate_tool",
		Description: "Tool with strict validation",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["required_field"],
			"properties": {
				"required_field": {"type": "string", "minLength": 1}
			}
		}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{}`),
		TimeoutMs:    1000,
	}

	_ = registry.Register(descriptor)

	// Missing required field
	invalidArgs := json.RawMessage(`{}`)
	_, err := registry.Execute("validate_tool", invalidArgs)
	if err == nil {
		t.Error("Execute should fail with invalid args")
	}

	// Empty required field
	invalidArgs2 := json.RawMessage(`{"required_field": ""}`)
	_, err = registry.Execute("validate_tool", invalidArgs2)
	if err == nil {
		t.Error("Execute should fail with empty required field")
	}
}

// TestExecute_ResultValidation verifies output validation
func TestExecute_ResultValidation(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:        "output_validate",
		Description: "Tool with output validation",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["status"],
			"properties": {
				"status": {"type": "string"}
			}
		}`),
		Mode:       "mock",
		MockResult: json.RawMessage(`{"status": "ok"}`),
		TimeoutMs:  1000,
	}

	_ = registry.Register(descriptor)

	args := json.RawMessage(`{}`)
	result, err := registry.Execute("output_validate", args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Error != "" {
		t.Errorf("Expected no error, got '%s'", result.Error)
	}
}

// TestExecute_LatencyTracking verifies latency is tracked
func TestExecute_LatencyTracking(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "latency_tool",
		Description:  "Tool for latency testing",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{}`),
		TimeoutMs:    1000,
	}

	_ = registry.Register(descriptor)

	args := json.RawMessage(`{}`)
	result, err := registry.Execute("latency_tool", args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.LatencyMs < 0 {
		t.Errorf("Expected non-negative latency, got %d", result.LatencyMs)
	}

	// Latency should be reasonable (< 1 second for mock)
	if result.LatencyMs > 1000 {
		t.Errorf("Expected latency < 1000ms, got %d", result.LatencyMs)
	}
}

// TestDefaultTimeout verifies default timeout is applied
func TestDefaultTimeout(t *testing.T) {
	registry := tools.NewRegistry()

	// Tool with zero timeout should get default
	descriptor := &tools.ToolDescriptor{
		Name:         "timeout_tool",
		Description:  "Tool without timeout",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:         "mock",
		TimeoutMs:    0, // Should trigger default
	}

	jsonData, _ := json.Marshal(descriptor)

	err := registry.LoadToolFromBytes("tool.json", jsonData)
	if err != nil {
		t.Fatalf("LoadToolFromBytes failed: %v", err)
	}

	tool := registry.Get("timeout_tool")
	if tool.TimeoutMs != 3000 {
		t.Errorf("Expected default timeout 3000ms, got %d", tool.TimeoutMs)
	}
}

// TestValidateDescriptor_InvalidSchema verifies schema validation
func TestValidateDescriptor_InvalidSchema(t *testing.T) {
	registry := tools.NewRegistry()

	// Tool with invalid JSON schema
	descriptor := map[string]interface{}{
		"name":        "invalid_schema",
		"description": "Tool with invalid schema",
		"inputSchema": "not a valid schema",
		"outputSchema": map[string]interface{}{
			"type": "object",
		},
		"mode":      "mock",
		"timeoutMs": 1000,
	}

	jsonData, _ := json.Marshal(descriptor)

	err := registry.LoadToolFromBytes("tool.json", jsonData)
	if err == nil {
		t.Error("LoadToolFromBytes should fail with invalid schema")
	}
}
