package tools_test

import (
	"context"
	"encoding/json"
	"os"
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
	result, err := staticExec.Execute(context.Background(), desc, args)
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

	// Test static execution from file
	file := t.TempDir() + "/mock.json"
	if err := os.WriteFile(file, []byte(`{"result":"file response"}`), 0o600); err != nil {
		t.Fatalf("failed to write mock file: %v", err)
	}
	descFile := &tools.ToolDescriptor{
		Name:           "fileTool",
		Mode:           "mock",
		MockResultFile: file,
	}
	result, err = staticExec.Execute(context.Background(), descFile, args)
	if err != nil {
		t.Fatalf("Static file execution failed: %v", err)
	}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("Failed to parse file result: %v", err)
	}
	if resultMap["result"] != "file response" {
		t.Errorf("Expected 'file response', got '%v'", resultMap["result"])
	}

	// Test scripted execution with template file
	tmplFile := t.TempDir() + "/tmpl.tmpl"
	if err := os.WriteFile(tmplFile, []byte(`{"greeting":"Hello {{.name}}"}`), 0o600); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}
	descTmpl := &tools.ToolDescriptor{
		Name:             "tmplTool",
		Mode:             "mock",
		MockTemplateFile: tmplFile,
	}
	scriptedArgs := json.RawMessage(`{"name":"Alice"}`)
	result, err = scriptedExec.Execute(context.Background(), descTmpl, scriptedArgs)
	if err != nil {
		t.Fatalf("Scripted file execution failed: %v", err)
	}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("Failed to parse template result: %v", err)
	}
	if resultMap["greeting"] != "Hello Alice" {
		t.Errorf("Expected 'Hello Alice', got '%v'", resultMap["greeting"])
	}

	t.Run("inline template beats file", func(t *testing.T) {
		file := t.TempDir() + "/tmpl.tmpl"
		if err := os.WriteFile(file, []byte(`{"greeting":"Hello File"}`), 0o600); err != nil {
			t.Fatalf("failed to write template file: %v", err)
		}
		descInline := &tools.ToolDescriptor{
			Name:             "inlineWins",
			Mode:             "mock",
			MockTemplate:     `{"greeting":"Hello {{.name}} from inline"}`,
			MockTemplateFile: file,
		}
		result, err := scriptedExec.Execute(context.Background(), descInline, scriptedArgs)
		if err != nil {
			t.Fatalf("Inline template execution failed: %v", err)
		}
		if err := json.Unmarshal(result, &resultMap); err != nil {
			t.Fatalf("Failed to parse inline template result: %v", err)
		}
		if resultMap["greeting"] != "Hello Alice from inline" {
			t.Errorf("Expected inline template to win, got '%v'", resultMap["greeting"])
		}
	})

	t.Run("non-JSON template wraps as result", func(t *testing.T) {
		descText := &tools.ToolDescriptor{
			Name:         "textTmpl",
			Mode:         "mock",
			MockTemplate: "hello {{.name}}",
		}
		result, err := scriptedExec.Execute(context.Background(), descText, scriptedArgs)
		if err != nil {
			t.Fatalf("Text template execution failed: %v", err)
		}
		if err := json.Unmarshal(result, &resultMap); err != nil {
			t.Fatalf("Failed to parse wrapped text result: %v", err)
		}
		if resultMap["result"] != "hello Alice" {
			t.Errorf("Expected wrapped text result, got '%v'", resultMap["result"])
		}
	})
}

func TestMockExecutorErrors(t *testing.T) {
	scriptedExec := tools.NewMockScriptedExecutor()
	args := json.RawMessage(`{"name":"Bob"}`)

	t.Run("missing template", func(t *testing.T) {
		desc := &tools.ToolDescriptor{Name: "noTemplate", Mode: "mock"}
		if _, err := scriptedExec.Execute(context.Background(), desc, args); err == nil {
			t.Fatalf("expected error for missing template")
		}
	})

	t.Run("bad template parse", func(t *testing.T) {
		desc := &tools.ToolDescriptor{
			Name:         "badTmpl",
			Mode:         "mock",
			MockTemplate: "{{ .name ", // malformed
		}
		if _, err := scriptedExec.Execute(context.Background(), desc, args); err == nil {
			t.Fatalf("expected template parse error")
		}
	})

	t.Run("bad template file read", func(t *testing.T) {
		desc := &tools.ToolDescriptor{
			Name:             "missingFile",
			Mode:             "mock",
			MockTemplateFile: "/does/not/exist.tmpl",
		}
		if _, err := scriptedExec.Execute(context.Background(), desc, args); err == nil {
			t.Fatalf("expected file read error")
		}
	})

	t.Run("bad json args", func(t *testing.T) {
		desc := &tools.ToolDescriptor{
			Name:         "tmpl",
			Mode:         "mock",
			MockTemplate: `{"hello":"{{.name}}"}`,
		}
		if _, err := scriptedExec.Execute(context.Background(), desc, json.RawMessage(`{"name":`)); err == nil {
			t.Fatalf("expected arg parse error")
		}
	})

	t.Run("missing mock result file", func(t *testing.T) {
		staticExec := tools.NewMockStaticExecutor()
		desc := &tools.ToolDescriptor{
			Name:           "missingFile",
			Mode:           "mock",
			MockResultFile: "/does/not/exist.json",
		}
		if _, err := staticExec.Execute(context.Background(), desc, nil); err == nil {
			t.Fatalf("expected missing file error")
		}
	})
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
