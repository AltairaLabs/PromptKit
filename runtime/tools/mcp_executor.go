package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

// MCPExecutor executes tools using MCP (Model Context Protocol) servers
type MCPExecutor struct {
	registry mcp.Registry
	ctx      context.Context
}

// NewMCPExecutor creates a new MCP executor
func NewMCPExecutor(registry mcp.Registry) *MCPExecutor {
	return &MCPExecutor{
		registry: registry,
		ctx:      context.Background(),
	}
}

// Name returns the executor name
func (e *MCPExecutor) Name() string {
	return "mcp"
}

// Execute executes a tool using an MCP server
func (e *MCPExecutor) Execute(descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	if descriptor.Mode != "mcp" {
		return nil, fmt.Errorf("MCP executor can only execute mcp tools")
	}

	// Log the tool call
	logger.Info("🔧 MCP Tool Call", "tool", descriptor.Name, "args", string(args))

	// Get the MCP client for this tool
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()

	client, err := e.registry.GetClientForTool(ctx, descriptor.Name)
	if err != nil {
		logger.Error("❌ MCP Tool Failed", "tool", descriptor.Name, "error", err)
		return nil, fmt.Errorf("failed to get MCP client for tool %s: %w", descriptor.Name, err)
	}

	// Call the tool via MCP
	response, err := client.CallTool(ctx, descriptor.Name, args)
	if err != nil {
		logger.Error("❌ MCP Tool Failed", "tool", descriptor.Name, "error", err)
		return nil, fmt.Errorf("MCP tool call failed: %w", err)
	}

	// Check for tool errors - extract detailed error message
	if response.IsError {
		// Build detailed error message from response content
		errorMsg := "MCP tool returned error"
		if len(response.Content) > 0 {
			for i, content := range response.Content {
				if content.Text != "" {
					if i == 0 {
						errorMsg = content.Text
					} else {
						errorMsg += "; " + content.Text
					}
				}
			}
		}
		logger.Error("❌ MCP Tool Error", "tool", descriptor.Name, "error", errorMsg)
		return nil, fmt.Errorf("%s", errorMsg)
	}

	// Extract text content from the response for the LLM
	// MCP responses contain an array of content items, each with a type and text
	if len(response.Content) == 0 {
		// Empty response - return success message
		logger.Info("✅ MCP Tool Success", "tool", descriptor.Name, "result", "empty (success)")
		return json.Marshal("Operation completed successfully")
	}

	// If single text content, return it directly
	if len(response.Content) == 1 && response.Content[0].Type == "text" {
		result := response.Content[0].Text
		logger.Info("✅ MCP Tool Success", "tool", descriptor.Name, "result_length", len(result))
		return json.Marshal(result)
	}

	// Multiple content items - concatenate them
	var contentParts []string
	for _, content := range response.Content {
		if content.Type == "text" && content.Text != "" {
			contentParts = append(contentParts, content.Text)
		}
	}

	if len(contentParts) > 0 {
		logger.Info("✅ MCP Tool Success", "tool", descriptor.Name, "content_parts", len(contentParts))
		return json.Marshal(contentParts)
	}

	// Fallback - return the full response structure
	logger.Info("✅ MCP Tool Success", "tool", descriptor.Name, "result_type", "structured")
	return json.Marshal(response.Content)
}
