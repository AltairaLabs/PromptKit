package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestToolRegistry(t *testing.T) {
	registry := tools.NewRegistry()

	// Test basic functionality
	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}

	// Test loading a tool descriptor
	desc := &tools.ToolDescriptor{
		Name:         "testTool",
		Description:  "A test tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"test": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "mock",
		TimeoutMs:    1000,
	}

	err := registry.Register(desc)
	if err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	// Test getting the tool back
	retrieved := registry.Get("testTool")
	if retrieved == nil {
		t.Fatal("Failed to retrieve registered tool")
	}

	if retrieved.Name != "testTool" {
		t.Errorf("Expected tool name 'testTool', got '%s'", retrieved.Name)
	}
}

func TestMockExecutors(t *testing.T) {
	staticExec := tools.NewMockStaticExecutor()
	if staticExec.Name() != "mock-static" {
		t.Errorf("Expected executor name 'mock-static', got '%s'", staticExec.Name())
	}

	scriptedExec := tools.NewMockScriptedExecutor()
	if scriptedExec.Name() != "mock-scripted" {
		t.Errorf("Expected executor name 'mock-scripted', got '%s'", scriptedExec.Name())
	}

	// Test static execution
	desc := &tools.ToolDescriptor{
		Name:       "testTool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"result": "static response"}`),
	}

	args := json.RawMessage(`{"input": "test"}`)
	result, err := staticExec.Execute(desc, args)
	if err != nil {
		t.Fatalf("Static execution failed: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if resultMap["result"] != "static response" {
		t.Errorf("Expected 'static response', got '%v'", resultMap["result"])
	}
}

func TestSchemaValidator(t *testing.T) {
	validator := tools.NewSchemaValidator()

	// Create a simple schema
	desc := &tools.ToolDescriptor{
		Name: "testTool",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["name"],
			"properties": {
				"name": {"type": "string", "minLength": 1}
			}
		}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["greeting"],
			"properties": {
				"greeting": {"type": "string"}
			}
		}`),
	}

	// Test valid args
	validArgs := json.RawMessage(`{"name": "test"}`)
	err := validator.ValidateArgs(desc, validArgs)
	if err != nil {
		t.Errorf("Valid args should not fail validation: %v", err)
	}

	// Test invalid args
	invalidArgs := json.RawMessage(`{"name": ""}`)
	err = validator.ValidateArgs(desc, invalidArgs)
	if err == nil {
		t.Error("Invalid args should fail validation")
	}

	// Test valid result
	validResult := json.RawMessage(`{"greeting": "Hello test"}`)
	err = validator.ValidateResult(desc, validResult)
	if err != nil {
		t.Errorf("Valid result should not fail validation: %v", err)
	}

	// Test invalid result
	invalidResult := json.RawMessage(`{"message": "Hello"}`)
	err = validator.ValidateResult(desc, invalidResult)
	if err == nil {
		t.Error("Invalid result should fail validation")
	}
}
