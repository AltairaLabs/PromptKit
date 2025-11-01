package mcp

import (
	"context"
	"encoding/json"
)

// Protocol version (as of 2025-06-18)
const ProtocolVersion = "2025-06-18"

// JSONRPCMessage represents a JSON-RPC 2.0 message
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`     // Request ID (number or string)
	Method  string          `json:"method,omitempty"` // Method name for requests/notifications
	Params  json.RawMessage `json:"params,omitempty"` // Parameters for method
	Result  json.RawMessage `json:"result,omitempty"` // Result for responses
	Error   *JSONRPCError   `json:"error,omitempty"`  // Error for error responses
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// InitializeRequest represents the initialization request params
type InitializeRequest struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// InitializeResponse represents the initialization response
type InitializeResponse struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// Implementation describes client or server implementation details
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities describes what the client supports
type ClientCapabilities struct {
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
	Sampling    *SamplingCapability    `json:"sampling,omitempty"`
	Logging     *LoggingCapability     `json:"logging,omitempty"`
}

// ServerCapabilities describes what the server supports
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability indicates the server supports tools
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"` // Server can send notifications
}

// ResourcesCapability indicates the server supports resources
type ResourcesCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates the server supports prompts
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ElicitationCapability indicates the client supports elicitation
type ElicitationCapability struct{}

// SamplingCapability indicates the client supports sampling
type SamplingCapability struct{}

// LoggingCapability indicates the client supports logging
type LoggingCapability struct{}

// ToolsListRequest represents a request to list available tools
type ToolsListRequest struct {
	// No parameters needed
}

// ToolsListResponse represents the response to a tools/list request
type ToolsListResponse struct {
	Tools []Tool `json:"tools"`
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"` // JSON Schema for tool input
}

// ToolCallRequest represents a request to execute a tool
type ToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResponse represents the response from a tool execution
type ToolCallResponse struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents a content item in MCP responses
type Content struct {
	Type     string `json:"type"` // "text", "image", "resource", etc.
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // Base64 encoded data
	MimeType string `json:"mimeType,omitempty"` // MIME type for data
	URI      string `json:"uri,omitempty"`      // URI for resources
}

// Client interface defines the MCP client operations
type Client interface {
	// Initialize establishes the MCP connection and negotiates capabilities
	Initialize(ctx context.Context) (*InitializeResponse, error)

	// ListTools retrieves all available tools from the server
	ListTools(ctx context.Context) ([]Tool, error)

	// CallTool executes a tool with the given arguments
	CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error)

	// Close terminates the connection to the MCP server
	Close() error

	// IsAlive checks if the connection is still active
	IsAlive() bool
}

// ServerConfig represents configuration for an MCP server
type ServerConfig struct {
	Name    string            `json:"name" yaml:"name"`       // Unique identifier for this server
	Command string            `json:"command" yaml:"command"` // Command to execute
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// Registry interface defines the MCP server registry operations
type Registry interface {
	// RegisterServer adds a new MCP server configuration
	RegisterServer(config ServerConfig) error

	// GetClient returns an active client for the given server name
	GetClient(ctx context.Context, serverName string) (Client, error)

	// GetClientForTool returns the client that provides the specified tool
	GetClientForTool(ctx context.Context, toolName string) (Client, error)

	// ListServers returns all registered server names
	ListServers() []string

	// ListAllTools returns all tools from all connected servers
	ListAllTools(ctx context.Context) (map[string][]Tool, error)

	// Close shuts down all MCP servers and connections
	Close() error
}
