package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

const (
	modeMock             = "mock"
	modeLive             = "live"
	modeMCP              = "mcp"
	executorMockStatic   = "mock-static"
	executorMockScripted = "mock-scripted"
)

// MockStaticExecutor executes tools using static mock data
type MockStaticExecutor struct{}

// NewMockStaticExecutor creates a new static mock executor
func NewMockStaticExecutor() *MockStaticExecutor {
	return &MockStaticExecutor{}
}

// Name returns the executor name
func (e *MockStaticExecutor) Name() string {
	return executorMockStatic
}

// Execute executes a tool using static mock data
func (e *MockStaticExecutor) Execute(descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	if descriptor.Mode != modeMock {
		return nil, fmt.Errorf("static mock executor can only execute mock tools")
	}

	// Use the mock result from the descriptor
	if len(descriptor.MockResult) > 0 {
		return descriptor.MockResult, nil
	}

	// Load mock result from file if specified
	if descriptor.MockResultFile != "" {
		data, err := os.ReadFile(filepath.Clean(descriptor.MockResultFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read mock_result_file for tool %s: %w", descriptor.Name, err)
		}
		return json.RawMessage(data), nil
	}

	// Generate a basic mock response if no mock_result is specified
	return json.RawMessage(`{"result": "mock response"}`), nil
}

// MockScriptedExecutor executes tools using templated mock data
type MockScriptedExecutor struct{}

// NewMockScriptedExecutor creates a new scripted mock executor
func NewMockScriptedExecutor() *MockScriptedExecutor {
	return &MockScriptedExecutor{}
}

// Name returns the executor name
func (e *MockScriptedExecutor) Name() string {
	return executorMockScripted
}

// Execute executes a tool using templated mock data
func (e *MockScriptedExecutor) Execute(descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	if descriptor.Mode != modeMock {
		return nil, fmt.Errorf("scripted mock executor can only execute mock tools")
	}

	// Choose template source
	tmpl := descriptor.MockTemplate
	if tmpl == "" && descriptor.MockTemplateFile != "" {
		data, err := os.ReadFile(filepath.Clean(descriptor.MockTemplateFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read mock_template_file for tool %s: %w", descriptor.Name, err)
		}
		tmpl = string(data)
	}

	if tmpl == "" {
		return nil, fmt.Errorf("no mock template specified for tool %s", descriptor.Name)
	}

	// Parse arguments for template processing
	var argsMap map[string]interface{}
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse arguments for templating: %w", err)
	}

	// Render template using text/template for basic conditional logic
	result, err := e.processTemplate(tmpl, argsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to render mock template for tool %s: %w", descriptor.Name, err)
	}

	// Try to parse as JSON to validate
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(result), &jsonResult); err != nil {
		// If not valid JSON, wrap it as a string value
		jsonResult = map[string]interface{}{
			"result": result,
		}
	}

	return json.Marshal(jsonResult)
}

// processTemplate renders a Go text/template with provided arguments.
func (e *MockScriptedExecutor) processTemplate(tmpl string, args map[string]interface{}) (string, error) {
	t, err := template.New("mock").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, args); err != nil {
		return "", err
	}
	return out.String(), nil
}
