package tools_test

import (
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
) // TestNewRegistryWithRepository_Preloading tests that tools are preloaded from repository
func TestNewRegistryWithRepository_Preloading(t *testing.T) {
	// Create a memory repository and add tools directly
	repo := memory.NewToolRepository()

	// Add test tools to repository
	tool1 := &tools.ToolDescriptor{
		Name:        "test_tool_1",
		Description: "First test tool",
		Mode:        "mock",
		TimeoutMs:   3000,
	}

	err := repo.SaveTool(tool1)
	require.NoError(t, err)

	// Create registry with repository - tools should be preloaded
	registry := tools.NewRegistryWithRepository(repo)

	// Verify tools are immediately available from cache
	toolsMap := registry.GetTools()
	assert.Len(t, toolsMap, 1)

	// Verify specific tool is loaded
	loadedTool1, exists := toolsMap["test_tool_1"]
	assert.True(t, exists)
	assert.Equal(t, "First test tool", loadedTool1.Description)
}

// TestNewRegistryWithRepository_EmptyRepository tests preloading with empty repository
func TestNewRegistryWithRepository_EmptyRepository(t *testing.T) {
	// Create empty memory repository
	repo := memory.NewToolRepository()

	// Create registry with empty repository
	registry := tools.NewRegistryWithRepository(repo)

	// Should have no tools
	toolsMap := registry.GetTools()
	assert.Len(t, toolsMap, 0)

	names := registry.List()
	assert.Len(t, names, 0)
}

// ErrorMockRepository is a test helper that always returns errors
type ErrorMockRepository struct{}

func (r *ErrorMockRepository) LoadTool(name string) (*tools.ToolDescriptor, error) {
	return nil, assert.AnError
}

func (r *ErrorMockRepository) ListTools() ([]string, error) {
	return nil, assert.AnError
}

func (r *ErrorMockRepository) SaveTool(descriptor *tools.ToolDescriptor) error {
	return assert.AnError
}

// TestNewRegistryWithRepository_RepositoryError tests preloading with repository errors
func TestNewRegistryWithRepository_RepositoryError(t *testing.T) {
	// Create a mock repository that returns errors
	errorRepo := &ErrorMockRepository{}

	// Create registry - should not panic even if repository has errors
	registry := tools.NewRegistryWithRepository(errorRepo)

	// Should gracefully handle errors and have no tools loaded
	toolsMap := registry.GetTools()
	assert.Len(t, toolsMap, 0)

	names := registry.List()
	assert.Len(t, names, 0)
}

// TestRegistryPreloadingIntegration tests the complete flow of loading tools via repository
func TestRegistryPreloadingIntegration(t *testing.T) {
	// This mimics the flow in arena engine builder
	repo := memory.NewToolRepository()

	// Simulate loading tool data from files (like arena does)
	toolYAML := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: integration_tool
spec:
  name: integration_tool
  description: Integration test tool
  mode: mock
  timeout_ms: 3000
  input_schema:
    type: object
    properties:
      input:
        type: string
  output_schema:
    type: object
    properties:
      output:
        type: string
  mock_response:
    result: "Mock integration result"
`

	// Parse tool like arena does
	tempRegistry := tools.NewRegistry()
	err := tempRegistry.LoadToolFromBytes("integration_tool.yaml", []byte(toolYAML))
	require.NoError(t, err)

	// Get parsed tools and save to repository
	parsedTools := tempRegistry.GetTools()
	require.Len(t, parsedTools, 1)

	for _, tool := range parsedTools {
		err = repo.SaveTool(tool)
		require.NoError(t, err)
	}

	// Create final registry with repository (like arena does)
	finalRegistry := tools.NewRegistryWithRepository(repo)

	// Verify tool is immediately available
	toolsMap := finalRegistry.GetTools()
	assert.Len(t, toolsMap, 1)

	tool, exists := toolsMap["integration_tool"]
	assert.True(t, exists)
	assert.Equal(t, "Integration test tool", tool.Description)
	assert.Equal(t, "mock", tool.Mode)

	// Verify GetToolsByNames works with preloaded tools
	selectedTools, err := finalRegistry.GetToolsByNames([]string{"integration_tool"})
	require.NoError(t, err)
	assert.Len(t, selectedTools, 1)
	assert.Equal(t, "integration_tool", selectedTools[0].Name)
}

// TestRegistryPreloading_MultipleTools tests preloading with multiple tools
func TestRegistryPreloading_MultipleTools(t *testing.T) {
	repo := memory.NewToolRepository()

	// Add multiple tools
	for i := 0; i < 5; i++ {
		tool := &tools.ToolDescriptor{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: fmt.Sprintf("Test tool %d", i),
			Mode:        "mock",
			TimeoutMs:   1000 + i*100,
		}
		err := repo.SaveTool(tool)
		require.NoError(t, err)
	}

	// Create registry with repository
	registry := tools.NewRegistryWithRepository(repo)

	// Verify all tools are preloaded
	toolsMap := registry.GetTools()
	assert.Len(t, toolsMap, 5)

	// Verify each tool
	for i := 0; i < 5; i++ {
		toolName := fmt.Sprintf("tool_%d", i)
		tool, exists := toolsMap[toolName]
		assert.True(t, exists, "Tool %s should exist", toolName)
		assert.Equal(t, fmt.Sprintf("Test tool %d", i), tool.Description)
		assert.Equal(t, 1000+i*100, tool.TimeoutMs)
	}

	// Verify List() returns all tools
	names := registry.List()
	assert.Len(t, names, 5)
	for i := 0; i < 5; i++ {
		assert.Contains(t, names, fmt.Sprintf("tool_%d", i))
	}
}
