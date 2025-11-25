package tools

import (
	"encoding/json"
	"fmt"
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

	if descriptor.MockTemplate == "" {
		return nil, fmt.Errorf("no mock template specified for tool %s", descriptor.Name)
	}

	// Parse arguments for template processing
	var argsMap map[string]interface{}
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse arguments for templating: %w", err)
	}

	// Simple template processing - replace {{.field}} with values
	result := e.processTemplate(descriptor.MockTemplate, argsMap)

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

// processTemplate performs simple template substitution
func (e *MockScriptedExecutor) processTemplate(template string, args map[string]interface{}) string {
	result := template

	// Simple replacement of {{.field}} patterns
	for key, value := range args {
		placeholder := fmt.Sprintf("{{ .%s }}", key)
		if valueStr, ok := value.(string); ok {
			result = replaceAll(result, placeholder, valueStr)
		} else {
			result = replaceAll(result, placeholder, fmt.Sprintf("%v", value))
		}
	}

	return result
}

// replaceAll replaces all occurrences of old with newStr in s
func replaceAll(s, old, newStr string) string {
	// Simple string replacement implementation
	for {
		result := replace(s, old, newStr)
		if result == s {
			break
		}
		s = result
	}
	return s
}

// replace replaces the first occurrence of old with newStr in s
func replace(s, old, newStr string) string {
	if old == "" {
		return s
	}

	index := indexOf(s, old)
	if index == -1 {
		return s
	}

	return s[:index] + newStr + s[index+len(old):]
}

// indexOf returns the index of the first occurrence of substr in s
func indexOf(s, substr string) int {
	if substr == "" {
		return 0
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}
