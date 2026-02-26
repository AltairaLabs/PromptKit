package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepositoryToolExecutor(t *testing.T) {
	baseExec := NewMockStaticExecutor()
	repo := NewInMemoryToolResponseRepository()

	executor := NewRepositoryToolExecutor(baseExec, repo, "test-context")

	assert.NotNil(t, executor)
	assert.Equal(t, "mock-static-with-repository", executor.Name())
	assert.Equal(t, baseExec, executor.baseExecutor)
	assert.Equal(t, repo, executor.repository)
	assert.Equal(t, "test-context", executor.contextKey)
}

func TestRepositoryToolExecutor_Execute_RepositoryResponse(t *testing.T) {
	// Create base executor and repository
	baseExec := NewMockStaticExecutor()
	repo := NewInMemoryToolResponseRepository()

	// Add a repository response
	repo.AddResponse("test-context", "test_tool", &ToolResponseData{
		Result: map[string]interface{}{
			"temperature": 72,
			"condition":   "sunny",
		},
	})

	executor := NewRepositoryToolExecutor(baseExec, repo, "test-context")

	// Create tool descriptor
	descriptor := &ToolDescriptor{
		Name:        "test_tool",
		Description: "Test tool",
		Mode:        "mock",
		MockResult:  json.RawMessage(`{"fallback": "base executor result"}`),
	}

	args := json.RawMessage(`{"location": "San Francisco"}`)

	// Execute - should use repository response
	result, err := executor.Execute(context.Background(), descriptor, args)

	require.NoError(t, err)

	var response map[string]interface{}
	err = json.Unmarshal(result, &response)
	require.NoError(t, err)

	assert.Equal(t, 72.0, response["temperature"])
	assert.Equal(t, "sunny", response["condition"])
	// Should NOT contain the base executor fallback
	assert.NotContains(t, response, "fallback")
}

func TestRepositoryToolExecutor_Execute_FallbackToBase(t *testing.T) {
	// Create base executor and empty repository
	baseExec := NewMockStaticExecutor()
	repo := NewInMemoryToolResponseRepository()

	executor := NewRepositoryToolExecutor(baseExec, repo, "test-context")

	// Create tool descriptor
	descriptor := &ToolDescriptor{
		Name:        "test_tool",
		Description: "Test tool",
		Mode:        "mock",
		MockResult:  json.RawMessage(`{"fallback": "base executor result"}`),
	}

	args := json.RawMessage(`{"location": "San Francisco"}`)

	// Execute - should fall back to base executor since no repository response
	result, err := executor.Execute(context.Background(), descriptor, args)

	require.NoError(t, err)

	var response map[string]interface{}
	err = json.Unmarshal(result, &response)
	require.NoError(t, err)

	assert.Equal(t, "base executor result", response["fallback"])
}

func TestRepositoryToolExecutor_Execute_RepositoryError(t *testing.T) {
	// Create base executor and repository
	baseExec := NewMockStaticExecutor()
	repo := NewInMemoryToolResponseRepository()

	// Add an error response
	repo.AddResponse("test-context", "test_tool", &ToolResponseData{
		Error: &ToolErrorData{
			Type:    "NotFound",
			Message: "Resource not found",
		},
	})

	executor := NewRepositoryToolExecutor(baseExec, repo, "test-context")

	// Create tool descriptor
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		Mode: "mock",
	}

	args := json.RawMessage(`{"id": "123"}`)

	// Execute - should return repository error
	_, err := executor.Execute(context.Background(), descriptor, args)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "NotFound: Resource not found")
}

func TestRepositoryToolExecutor_Execute_WrongContext(t *testing.T) {
	// Create base executor and repository
	baseExec := NewMockStaticExecutor()
	repo := NewInMemoryToolResponseRepository()

	// Add response for different context
	repo.AddResponse("different-context", "test_tool", &ToolResponseData{
		Result: map[string]interface{}{"should_not_see": "this"},
	})

	executor := NewRepositoryToolExecutor(baseExec, repo, "test-context")

	// Create tool descriptor
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"fallback": "base executor"}`),
	}

	args := json.RawMessage(`{"test": "value"}`)

	// Execute - should fall back to base executor due to context mismatch
	result, err := executor.Execute(context.Background(), descriptor, args)

	require.NoError(t, err)

	var response map[string]interface{}
	err = json.Unmarshal(result, &response)
	require.NoError(t, err)

	assert.Equal(t, "base executor", response["fallback"])
	assert.NotContains(t, response, "should_not_see")
}

func TestRepositoryToolExecutor_Execute_InvalidJSON(t *testing.T) {
	// Create base executor and repository
	baseExec := NewMockStaticExecutor()
	repo := NewInMemoryToolResponseRepository()

	executor := NewRepositoryToolExecutor(baseExec, repo, "test-context")

	// Create tool descriptor
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"fallback": "base executor"}`),
	}

	// Invalid JSON arguments
	args := json.RawMessage(`{invalid json}`)

	// Execute - should fall back to base executor due to JSON parse error
	result, err := executor.Execute(context.Background(), descriptor, args)

	require.NoError(t, err)

	var response map[string]interface{}
	err = json.Unmarshal(result, &response)
	require.NoError(t, err)

	assert.Equal(t, "base executor", response["fallback"])
}

func TestFileToolResponseRepository_GetToolResponse_Success(t *testing.T) {
	// Create repository with tool responses
	toolResponses := map[string][]MockToolResponseConfig{
		"get_weather": {
			{
				CallArgs: map[string]interface{}{
					"location": "San Francisco",
					"unit":     "celsius",
				},
				Result: map[string]interface{}{
					"temperature": 20,
					"condition":   "cloudy",
				},
			},
		},
	}

	repo := NewFileToolResponseRepository("test-scenario", toolResponses)

	// Test matching arguments
	args := map[string]interface{}{
		"location": "San Francisco",
		"unit":     "celsius",
	}

	response, err := repo.GetToolResponse("get_weather", args, "test-scenario")

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Nil(t, response.Error)

	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 20, result["temperature"])
	assert.Equal(t, "cloudy", result["condition"])
}

func TestFileToolResponseRepository_GetToolResponse_ArgumentMismatch(t *testing.T) {
	// Create repository with tool responses
	toolResponses := map[string][]MockToolResponseConfig{
		"get_weather": {
			{
				CallArgs: map[string]interface{}{
					"location": "San Francisco",
					"unit":     "celsius",
				},
				Result: map[string]interface{}{"temperature": 20},
			},
		},
	}

	repo := NewFileToolResponseRepository("test-scenario", toolResponses)

	// Test with different arguments
	args := map[string]interface{}{
		"location": "New York", // Different location
		"unit":     "celsius",
	}

	response, err := repo.GetToolResponse("get_weather", args, "test-scenario")

	require.NoError(t, err)
	assert.Nil(t, response) // No matching response
}

func TestFileToolResponseRepository_GetToolResponse_ContextMismatch(t *testing.T) {
	toolResponses := map[string][]MockToolResponseConfig{
		"get_weather": {
			{
				CallArgs: map[string]interface{}{"location": "SF"},
				Result:   map[string]interface{}{"temp": 70},
			},
		},
	}

	repo := NewFileToolResponseRepository("test-scenario", toolResponses)

	args := map[string]interface{}{"location": "SF"}

	// Different context
	response, err := repo.GetToolResponse("get_weather", args, "different-scenario")

	require.NoError(t, err)
	assert.Nil(t, response) // Context mismatch
}

func TestFileToolResponseRepository_GetToolResponse_ErrorResponse(t *testing.T) {
	toolResponses := map[string][]MockToolResponseConfig{
		"failing_tool": {
			{
				CallArgs: map[string]interface{}{"id": "invalid"},
				Error: &MockToolErrorConfig{
					Type:    "ValidationError",
					Message: "Invalid ID provided",
				},
			},
		},
	}

	repo := NewFileToolResponseRepository("test-scenario", toolResponses)

	args := map[string]interface{}{"id": "invalid"}

	response, err := repo.GetToolResponse("failing_tool", args, "test-scenario")

	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, response.Error)
	assert.Equal(t, "ValidationError", response.Error.Type)
	assert.Equal(t, "Invalid ID provided", response.Error.Message)
	assert.Nil(t, response.Result)
}

func TestFileToolResponseRepository_ArgumentsMatch(t *testing.T) {
	repo := &FileToolResponseRepository{}

	tests := []struct {
		name     string
		provided map[string]interface{}
		expected map[string]interface{}
		want     bool
	}{
		{
			name:     "exact match",
			provided: map[string]interface{}{"a": 1, "b": "test"},
			expected: map[string]interface{}{"a": 1, "b": "test"},
			want:     true,
		},
		{
			name:     "extra provided args (should match)",
			provided: map[string]interface{}{"a": 1, "b": "test", "c": "extra"},
			expected: map[string]interface{}{"a": 1, "b": "test"},
			want:     true,
		},
		{
			name:     "missing required arg",
			provided: map[string]interface{}{"a": 1},
			expected: map[string]interface{}{"a": 1, "b": "test"},
			want:     false,
		},
		{
			name:     "value mismatch",
			provided: map[string]interface{}{"a": 1, "b": "different"},
			expected: map[string]interface{}{"a": 1, "b": "test"},
			want:     false,
		},
		{
			name:     "empty expected (matches anything)",
			provided: map[string]interface{}{"a": 1, "b": "test"},
			expected: map[string]interface{}{},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := repo.argumentsMatch(tt.provided, tt.expected)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestInMemoryToolResponseRepository(t *testing.T) {
	repo := NewInMemoryToolResponseRepository()

	// Test empty repository
	response, err := repo.GetToolResponse("nonexistent", nil, "test")
	require.NoError(t, err)
	assert.Nil(t, response)

	// Add response
	expectedResponse := &ToolResponseData{
		Result: map[string]interface{}{"value": 42},
	}
	repo.AddResponse("test-context", "test_tool", expectedResponse)

	// Test retrieval
	response, err = repo.GetToolResponse("test_tool", nil, "test-context")
	require.NoError(t, err)
	assert.Equal(t, expectedResponse, response)

	// Test wrong context
	response, err = repo.GetToolResponse("test_tool", nil, "wrong-context")
	require.NoError(t, err)
	assert.Nil(t, response)

	// Test wrong tool name
	response, err = repo.GetToolResponse("wrong_tool", nil, "test-context")
	require.NoError(t, err)
	assert.Nil(t, response)
}
