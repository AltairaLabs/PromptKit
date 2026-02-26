package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

const (
	mcpToolSuccess      = "✅ MCP Tool Success"
	mcpToolReturnedErr  = "MCP tool returned error"
	mcpContentTypeText  = "text"
	mcpDefaultTimeoutSc = 30
)

// MCPExecutor executes tools using MCP (Model Context Protocol) servers
type MCPExecutor struct {
	registry mcp.Registry
}

// NewMCPExecutor creates a new MCP executor
func NewMCPExecutor(registry mcp.Registry) *MCPExecutor {
	return &MCPExecutor{
		registry: registry,
	}
}

// Name returns the executor name
func (e *MCPExecutor) Name() string {
	return modeMCP
}

// Execute executes a tool using an MCP server
func (e *MCPExecutor) Execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.Mode != modeMCP {
		return nil, ErrMCPExecutorOnly
	}

	logger.Info("🔧 MCP Tool Call", "tool", descriptor.Name, "args", string(args))

	response, err := e.callMCPTool(ctx, descriptor.Name, args)
	if err != nil {
		return nil, err
	}

	if response.IsError {
		return nil, e.handleErrorResponse(descriptor.Name, response)
	}

	return e.formatSuccessResponse(descriptor.Name, response)
}

func (e *MCPExecutor) callMCPTool(
	ctx context.Context, toolName string, args json.RawMessage,
) (*mcp.ToolCallResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpDefaultTimeoutSc*time.Second)
	defer cancel()

	// Use the raw MCP name (without namespace prefix) for server communication
	rawName := mcpRawToolName(toolName)

	client, err := e.registry.GetClientForTool(ctx, rawName)
	if err != nil {
		logger.Error("❌ MCP Tool Failed", "tool", toolName, "error", err)
		return nil, fmt.Errorf("failed to get MCP client for tool %s: %w", toolName, err)
	}

	response, err := client.CallTool(ctx, rawName, args)
	if err != nil {
		logger.Error("❌ MCP Tool Failed", "tool", toolName, "error", err)
		return nil, fmt.Errorf("MCP tool call failed: %w", err)
	}

	return response, nil
}

// mcpRawToolName strips the "mcp__server__" prefix from a qualified tool name,
// returning the original name the MCP server knows. If no "mcp__" prefix is
// present, the name is returned as-is for backward compatibility.
func mcpRawToolName(qualifiedName string) string {
	ns, rest, found := strings.Cut(qualifiedName, NamespaceSep)
	if !found || ns != "mcp" {
		return qualifiedName
	}
	// rest is "server__tool"; strip the server part
	_, rawName, found := strings.Cut(rest, NamespaceSep)
	if !found {
		return qualifiedName
	}
	return rawName
}

func (e *MCPExecutor) handleErrorResponse(toolName string, response *mcp.ToolCallResponse) error {
	errorMsg := e.extractErrorMessage(response.Content)
	logger.Error("❌ MCP Tool Error", "tool", toolName, "error", errorMsg)
	return fmt.Errorf("%s", errorMsg)
}

func (e *MCPExecutor) extractErrorMessage(content []mcp.Content) string {
	if len(content) == 0 {
		return mcpToolReturnedErr
	}

	var errorMsg string
	for i, item := range content {
		if item.Text != "" {
			if i == 0 {
				errorMsg = item.Text
			} else {
				errorMsg += "; " + item.Text
			}
		}
	}

	if errorMsg == "" {
		return mcpToolReturnedErr
	}
	return errorMsg
}

func (e *MCPExecutor) formatSuccessResponse(toolName string, response *mcp.ToolCallResponse) (json.RawMessage, error) {
	if len(response.Content) == 0 {
		logger.Info(mcpToolSuccess, "tool", toolName, "result", "empty (success)")
		return json.Marshal("Operation completed successfully")
	}

	if len(response.Content) == 1 && response.Content[0].Type == mcpContentTypeText {
		result := response.Content[0].Text
		logger.Info(mcpToolSuccess, "tool", toolName, "result_length", len(result))
		return json.Marshal(result)
	}

	contentParts := e.extractTextContent(response.Content)
	if len(contentParts) > 0 {
		logger.Info(mcpToolSuccess, "tool", toolName, "content_parts", len(contentParts))
		return json.Marshal(contentParts)
	}

	logger.Info(mcpToolSuccess, "tool", toolName, "result_type", "structured")
	return json.Marshal(response.Content)
}

func (e *MCPExecutor) extractTextContent(content []mcp.Content) []string {
	var contentParts []string
	for _, item := range content {
		if item.Type == "text" && item.Text != "" {
			contentParts = append(contentParts, item.Text)
		}
	}
	return contentParts
}
