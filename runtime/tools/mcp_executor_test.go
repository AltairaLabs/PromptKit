package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

// mockMCPClient implements the mcp.Client interface for testing
type mockMCPClient struct {
	callToolFunc func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error)
}

func (m *mockMCPClient) Initialize(ctx context.Context) (*mcp.InitializeResponse, error) {
	return &mcp.InitializeResponse{}, nil
}

func (m *mockMCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
	if m.callToolFunc != nil {
		return m.callToolFunc(ctx, name, args)
	}
	return &mcp.ToolCallResponse{
		Content: []mcp.Content{{Type: "text", Text: "success"}},
	}, nil
}

func (m *mockMCPClient) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	return nil, nil
}

func (m *mockMCPClient) Close() error {
	return nil
}

func (m *mockMCPClient) IsAlive() bool {
	return true
}

// mockMCPRegistry implements the mcp.Registry interface for testing
type mockMCPRegistry struct {
	getClientFunc func(ctx context.Context, toolName string) (mcp.Client, error)
}

func (m *mockMCPRegistry) RegisterServer(config mcp.ServerConfig) error {
	return nil
}

func (m *mockMCPRegistry) GetClient(ctx context.Context, serverName string) (mcp.Client, error) {
	return &mockMCPClient{}, nil
}

func (m *mockMCPRegistry) GetClientForTool(ctx context.Context, toolName string) (mcp.Client, error) {
	if m.getClientFunc != nil {
		return m.getClientFunc(ctx, toolName)
	}
	return &mockMCPClient{}, nil
}

func (m *mockMCPRegistry) ListServers() []string {
	return []string{}
}

func (m *mockMCPRegistry) ListAllTools(ctx context.Context) (map[string][]mcp.Tool, error) {
	return make(map[string][]mcp.Tool), nil
}

func (m *mockMCPRegistry) Close() error {
	return nil
}

func TestNewMCPExecutor(t *testing.T) {
	registry := &mockMCPRegistry{}
	executor := NewMCPExecutor(registry)

	if executor == nil {
		t.Fatal("NewMCPExecutor() returned nil")
	}

	if executor.Name() != modeMCP {
		t.Errorf("Name() = %q, want %q", executor.Name(), modeMCP)
	}
}

func TestMCPExecutor_Name(t *testing.T) {
	executor := NewMCPExecutor(&mockMCPRegistry{})
	if executor.Name() != modeMCP {
		t.Errorf("Name() = %q, want %q", executor.Name(), modeMCP)
	}
}

func TestMCPExecutor_Execute_Success(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{
							{Type: "text", Text: "Operation completed"},
						},
						IsError: false,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{"key":"value"}`)

	result, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	var resultStr string
	if err := json.Unmarshal(result, &resultStr); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if resultStr != "Operation completed" {
		t.Errorf("Execute() result = %q, want %q", resultStr, "Operation completed")
	}
}

func TestMCPExecutor_Execute_WrongMode(t *testing.T) {
	executor := NewMCPExecutor(&mockMCPRegistry{})
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		Mode: "http", // Wrong mode
	}
	args := json.RawMessage(`{}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err == nil {
		t.Error("Execute() with wrong mode should return error")
	}
}

func TestMCPExecutor_Execute_ClientNotFound(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return nil, errors.New("client not found")
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "nonexistent_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err == nil {
		t.Error("Execute() with nonexistent client should return error")
	}
}

func TestMCPExecutor_Execute_ToolCallFailed(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return nil, errors.New("tool execution failed")
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "failing_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err == nil {
		t.Error("Execute() with failing tool should return error")
	}
}

func TestMCPExecutor_Execute_ErrorResponse(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{
							{Type: "text", Text: "Error occurred"},
						},
						IsError: true,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "error_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err == nil {
		t.Error("Execute() with error response should return error")
	}
}

func TestMCPExecutor_Execute_EmptyResponse(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{},
						IsError: false,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "empty_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	result, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	var resultStr string
	if err := json.Unmarshal(result, &resultStr); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if resultStr != "Operation completed successfully" {
		t.Errorf("Execute() with empty response = %q, want %q", resultStr, "Operation completed successfully")
	}
}

func TestMCPExecutor_Execute_MultipleContentParts(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{
							{Type: "text", Text: "First part"},
							{Type: "text", Text: "Second part"},
						},
						IsError: false,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "multi_part_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	result, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	var parts []string
	if err := json.Unmarshal(result, &parts); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(parts) != 2 {
		t.Errorf("Execute() returned %d parts, want 2", len(parts))
	}
}

func TestMCPExecutor_Execute_StructuredResponse(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{
							{Type: "resource", Text: ""},
						},
						IsError: false,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "structured_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	result, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	var content []mcp.Content
	if err := json.Unmarshal(result, &content); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(content) != 1 {
		t.Errorf("Execute() returned %d content items, want 1", len(content))
	}
}

func TestMCPExecutor_Execute_ErrorWithMultipleMessages(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{
							{Type: "text", Text: "Error 1"},
							{Type: "text", Text: "Error 2"},
						},
						IsError: true,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "multi_error_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err == nil {
		t.Error("Execute() with multiple error messages should return error")
	}

	// Check that error contains both messages
	errMsg := err.Error()
	if errMsg != "Error 1; Error 2" {
		t.Errorf("Error message = %q, want %q", errMsg, "Error 1; Error 2")
	}
}

func TestMCPExecutor_Execute_ErrorWithEmptyContent(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{},
						IsError: true,
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "empty_error_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err == nil {
		t.Error("Execute() with empty error should return error")
	}

	errMsg := err.Error()
	if errMsg != "MCP tool returned error" {
		t.Errorf("Error message = %q, want %q", errMsg, "MCP tool returned error")
	}
}

func TestMCPExecutor_Execute_Timeout(t *testing.T) {
	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					// Simulate slow operation
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(100 * time.Millisecond):
						return &mcp.ToolCallResponse{
							Content: []mcp.Content{{Type: "text", Text: "success"}},
						}, nil
					}
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{
		Name: "timeout_tool",
		Mode: modeMCP,
	}
	args := json.RawMessage(`{}`)

	// This should not timeout (default is 30s)
	_, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() failed unexpectedly: %v", err)
	}
}

func TestMCPExecutor_ExtractTextContent_EmptyText(t *testing.T) {
	executor := NewMCPExecutor(&mockMCPRegistry{})

	content := []mcp.Content{
		{Type: "text", Text: ""},
		{Type: "text", Text: "Valid text"},
	}

	parts := executor.extractTextContent(content)
	if len(parts) != 1 {
		t.Errorf("extractTextContent() returned %d parts, want 1", len(parts))
	}
	if parts[0] != "Valid text" {
		t.Errorf("extractTextContent() = %q, want %q", parts[0], "Valid text")
	}
}

func TestMCPExecutor_ExtractTextContent_NonTextTypes(t *testing.T) {
	executor := NewMCPExecutor(&mockMCPRegistry{})

	content := []mcp.Content{
		{Type: "resource", Text: "Should be ignored"},
		{Type: "text", Text: "Valid text"},
		{Type: "image", Text: "Also ignored"},
	}

	parts := executor.extractTextContent(content)
	if len(parts) != 1 {
		t.Errorf("extractTextContent() returned %d parts, want 1", len(parts))
	}
	if parts[0] != "Valid text" {
		t.Errorf("extractTextContent() = %q, want %q", parts[0], "Valid text")
	}
}
