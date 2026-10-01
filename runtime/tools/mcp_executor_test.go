package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
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

func (m *mockMCPRegistry) UnregisterServer(name string) error {
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

func (m *mockMCPRegistry) GetServerConfig(serverName string) (mcp.ServerConfig, bool) {
	return mcp.ServerConfig{}, false
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

func TestMCPExecutor_Execute_ArgsNotLoggedAtInfo(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger.SetLogger(slog.New(handler))
	defer logger.SetLogger(nil) // reset

	registry := &mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return &mcp.ToolCallResponse{
						Content: []mcp.Content{{Type: "text", Text: "ok"}},
					}, nil
				},
			}, nil
		},
	}

	executor := NewMCPExecutor(registry)
	descriptor := &ToolDescriptor{Name: "test_tool", Mode: modeMCP}
	args := json.RawMessage(`{"password":"s3cret","ssn":"123-45-6789"}`)

	_, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	logOutput := buf.String()

	// The tool name SHOULD appear in logs
	if !bytes.Contains(buf.Bytes(), []byte("test_tool")) {
		t.Error("expected tool name to appear in INFO log output")
	}

	// The raw args (containing PII) should NOT appear at INFO level
	if bytes.Contains(buf.Bytes(), []byte("s3cret")) {
		t.Errorf("raw tool args with PII leaked into INFO log output: %s", logOutput)
	}
	if bytes.Contains(buf.Bytes(), []byte("123-45-6789")) {
		t.Errorf("raw tool args with PII leaked into INFO log output: %s", logOutput)
	}
}

func newStructuredContentExecutor(resp *mcp.ToolCallResponse) *MCPExecutor {
	return NewMCPExecutor(&mockMCPRegistry{
		getClientFunc: func(ctx context.Context, toolName string) (mcp.Client, error) {
			return &mockMCPClient{
				callToolFunc: func(ctx context.Context, name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
					return resp, nil
				},
			}, nil
		},
	})
}

func TestMCPExecutor_Execute_StructuredContent(t *testing.T) {
	descriptor := &ToolDescriptor{Name: "structured_tool", Mode: modeMCP}

	tests := []struct {
		name string
		resp *mcp.ToolCallResponse
		want string
	}{
		{
			name: "structuredContent only",
			resp: &mcp.ToolCallResponse{
				StructuredContent: json.RawMessage(`{"id":"checkout_abc123","status":"incomplete"}`),
			},
			want: `{"id":"checkout_abc123","status":"incomplete"}`,
		},
		{
			name: "structuredContent preferred over serialized text fallback",
			resp: &mcp.ToolCallResponse{
				Content:           []mcp.Content{{Type: "text", Text: `{"id":"checkout_abc123"}`}},
				StructuredContent: json.RawMessage(`{"id":"checkout_abc123"}`),
			},
			want: `{"id":"checkout_abc123"}`,
		},
		{
			name: "null structuredContent falls back to content",
			resp: &mcp.ToolCallResponse{
				Content:           []mcp.Content{{Type: "text", Text: "plain"}},
				StructuredContent: json.RawMessage(`null`),
			},
			want: `"plain"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := newStructuredContentExecutor(tt.resp).Execute(
				context.Background(), descriptor, json.RawMessage(`{}`))
			if err != nil {
				t.Fatalf("Execute() failed: %v", err)
			}
			if string(result) != tt.want {
				t.Errorf("Execute() result = %s, want %s", result, tt.want)
			}
		})
	}
}

func TestMCPExecutor_Execute_ErrorStructuredContent(t *testing.T) {
	descriptor := &ToolDescriptor{Name: "structured_tool", Mode: modeMCP}

	tests := []struct {
		name    string
		resp    *mcp.ToolCallResponse
		wantErr string
	}{
		{
			name: "structuredContent used when no text content",
			resp: &mcp.ToolCallResponse{
				IsError:           true,
				StructuredContent: json.RawMessage(`{"code":"out_of_stock"}`),
			},
			wantErr: `{"code":"out_of_stock"}`,
		},
		{
			name: "text content still wins",
			resp: &mcp.ToolCallResponse{
				IsError:           true,
				Content:           []mcp.Content{{Type: "text", Text: "Item out of stock"}},
				StructuredContent: json.RawMessage(`{"code":"out_of_stock"}`),
			},
			wantErr: "Item out of stock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newStructuredContentExecutor(tt.resp).Execute(
				context.Background(), descriptor, json.RawMessage(`{}`))
			if err == nil {
				t.Fatal("Execute() with error response should return error")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("Execute() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestFormatMCPResult_ContentBlocks(t *testing.T) {
	size := int64(42)
	resp := &mcp.ToolCallResponse{Content: []mcp.Content{
		{Type: mcp.ContentTypeText, Text: ""},
		{Type: mcp.ContentTypeText, Text: "see chart"},
		{Type: mcp.ContentTypeImage, Data: "aW1n", MimeType: "image/png"},
		{Type: mcp.ContentTypeAudio, Data: "YXVk", MimeType: "audio/wav"},
		{Type: mcp.ContentTypeResource, Resource: &mcp.ResourceContents{URI: "file:///a.txt", MimeType: "text/plain", Text: "SECRET BODY"}},
		{Type: mcp.ContentTypeResource, Resource: &mcp.ResourceContents{URI: "file:///p.png", MimeType: "image/png", Blob: "cG5n"}},
		{Type: mcp.ContentTypeResource, Resource: &mcp.ResourceContents{URI: "file:///b.bin", MimeType: "application/octet-stream", Blob: "YmlueQ=="}},
		{Type: mcp.ContentTypeResourceLink, URI: "file:///big.csv", Name: "big.csv", Description: "the data", Size: &size},
	}}

	result, parts, err := formatMCPResult("t", resp, false)
	require.NoError(t, err)
	assert.JSONEq(t, `[
		"see chart",
		{"type":"resource","uri":"file:///a.txt","mimeType":"text/plain","text":"SECRET BODY"},
		{"type":"resource","uri":"file:///b.bin","mimeType":"application/octet-stream","note":"binary content (8 base64 characters) not shown"},
		{"type":"resource_link","uri":"file:///big.csv","name":"big.csv","description":"the data","size":42}
	]`, string(result), "an empty text block is skipped; nothing else is dropped")
	require.Len(t, parts, 3, "image, audio and the embedded image reach the model as media parts")
	assert.Equal(t, types.ContentTypeImage, parts[0].Type)
	assert.Equal(t, "aW1n", *parts[0].Media.Data)
	assert.Equal(t, types.ContentTypeAudio, parts[1].Type)
	assert.Equal(t, types.ContentTypeImage, parts[2].Type)
	assert.Equal(t, "cG5n", *parts[2].Media.Data)
}

func TestFormatMCPResult_Shapes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []mcp.Content
		want    string
		parts   int
	}{
		{"empty", nil, `"Operation completed successfully"`, 0},
		{"one text stays a string", []mcp.Content{{Type: "text", Text: "a"}}, `"a"`, 0},
		{"several texts stay an array", []mcp.Content{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}, `["a","b"]`, 0},
		{"media only", []mcp.Content{{Type: "image", Data: "x", MimeType: "image/png"}}, `"Returned 1 media item(s)."`, 1},
		{"unknown block passes through", []mcp.Content{{Type: "future", Text: "t"}}, `[{"type":"future","text":"t"}]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, parts, err := formatMCPResult("t", &mcp.ToolCallResponse{Content: tc.content}, false)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(result))
			assert.Len(t, parts, tc.parts)
		})
	}
}

func TestMCPExecutor_ExecuteKeepsMediaInTheJSON(t *testing.T) {
	// Execute has no channel for parts, so media stays in the result.
	exec := newStructuredContentExecutor(&mcp.ToolCallResponse{Content: []mcp.Content{
		{Type: "text", Text: "chart:"}, {Type: "image", Data: "aW1n", MimeType: "image/png"},
	}})
	result, err := exec.Execute(context.Background(), &ToolDescriptor{Name: "t", Mode: modeMCP}, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `["chart:",{"type":"image","data":"aW1n","mimeType":"image/png"}]`, string(result))

	_, parts, err := exec.ExecuteMultimodal(context.Background(), &ToolDescriptor{Name: "t", Mode: modeMCP}, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Len(t, parts, 1)
}

func TestMCPExecutor_StructuredContentIsValidatedAgainstTheOutputSchema(t *testing.T) {
	descriptor := &ToolDescriptor{
		Name: "t", Mode: modeMCP,
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"temp":{"type":"number"}},"required":["temp"]}`),
	}
	ok := newStructuredContentExecutor(&mcp.ToolCallResponse{StructuredContent: json.RawMessage(`{"temp":21.5}`)})
	result, err := ok.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"temp":21.5}`, string(result))

	bad := newStructuredContentExecutor(&mcp.ToolCallResponse{StructuredContent: json.RawMessage(`{"temp":"warm"}`)})
	_, err = bad.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match its output schema")

	noSchema := &ToolDescriptor{Name: "t", Mode: modeMCP}
	_, err = bad.Execute(context.Background(), noSchema, json.RawMessage(`{}`))
	require.NoError(t, err, "without a declared schema there is nothing to validate against")
}

func TestSchemaValidator_NeverDereferencesAnExternalRef(t *testing.T) {
	// MCP SEP-2106: implementations MUST NOT dereference network $refs
	// automatically. gojsonschema would fetch them (or read file:// URLs).
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	defer srv.Close()

	for _, schema := range []string{
		`{"type":"object","properties":{"a":{"$ref":"` + srv.URL + `/s.json"}}}`,
		`{"type":"object","properties":{"a":{"$ref":"relative.json"}}}`,
		`{"type":"object","allOf":[{"$dynamicRef":"` + srv.URL + `/d.json"}]}`,
	} {
		r := NewRegistry()
		desc := &ToolDescriptor{Name: "t", Description: "d", Mode: modeMCP,
			InputSchema: json.RawMessage(schema), OutputSchema: json.RawMessage(schema)}
		require.NoError(t, r.Register(desc))
		assert.NoError(t, r.validator.ValidateArgs(desc, json.RawMessage(`{"a":1}`)), "validation is skipped, not failed")
		assert.NoError(t, r.validator.ValidateResult(desc, json.RawMessage(`{"a":1}`)))
		_, err := r.validator.getSchema(schema)
		assert.ErrorIs(t, err, errExternalSchemaRef)
	}
	assert.Zero(t, hits.Load(), "no external schema was fetched")

	local := `{"$defs":{"s":{"type":"string"}},"type":"object","properties":{"a":{"$ref":"#/$defs/s"}}}`
	desc := &ToolDescriptor{Name: "t", InputSchema: json.RawMessage(local)}
	assert.Error(t, NewSchemaValidator().ValidateArgs(desc, json.RawMessage(`{"a":1}`)), "local refs are still followed")
}
