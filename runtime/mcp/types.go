package mcp

import (
	"context"
	"encoding/json"
	"strings"
)

// ProtocolVersion defines the MCP protocol version (as of 2025-06-18).
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

// ToolFilter controls which tools from an MCP server are exposed to the LLM.
// If Allowlist is non-empty, only those tools are included.
// If Blocklist is non-empty, those tools are excluded.
// Allowlist takes precedence over Blocklist.
type ToolFilter struct {
	Allowlist []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Blocklist []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
}

// matchToolPattern reports whether a tool name matches a filter entry. An entry
// ending in "*" is a prefix match (e.g. "read_*" matches "read_file"); any other
// entry is an exact match.
func matchToolPattern(pattern, name string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(name, prefix)
	}
	return pattern == name
}

// Includes returns true if the given tool name passes the filter. Allowlist and
// blocklist entries may use a trailing-"*" prefix wildcard.
func (f ToolFilter) Includes(name string) bool {
	if len(f.Allowlist) > 0 {
		for _, a := range f.Allowlist {
			if matchToolPattern(a, name) {
				return true
			}
		}
		return false
	}
	for _, b := range f.Blocklist {
		if matchToolPattern(b, name) {
			return false
		}
	}
	return true
}

// ServerConfig represents configuration for an MCP server.
//
// Exactly one transport should be specified:
//   - Command: stdio transport — PromptKit spawns a local subprocess.
//   - URL:     HTTP transport — by default the legacy SSE adapter is used.
//     Set TransportName to TransportStreamableHTTP to opt into the
//     modern Streamable HTTP transport (MCP 2025-03-26).
//
// The registry selects the adapter via Transport(). Headers applies to all
// HTTP transports (SSE and Streamable HTTP).
type ServerConfig struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// WorkingDir sets the working directory for the server process (stdio only).
	WorkingDir string `json:"working_dir,omitempty" yaml:"working_dir,omitempty"`
	// URL is the base URL for an HTTP MCP server. When set without an
	// explicit TransportName, the registry uses the legacy SSE adapter for
	// back-compat. Set TransportName to TransportStreamableHTTP to opt into
	// the modern transport.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// Headers are sent on HTTP transports (both SSE and Streamable HTTP).
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// TransportName selects the transport adapter explicitly. When empty, the
	// legacy inference applies (URL → SSE, Command → Stdio) for back-compat.
	// Set to TransportStreamableHTTP to opt into the modern Streamable HTTP
	// transport against a URL.
	TransportName Transport `json:"transport,omitempty" yaml:"transport,omitempty"`
	// TimeoutMs sets the per-request timeout in milliseconds.
	TimeoutMs int `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	// ToolFilter controls which tools from this server are exposed.
	ToolFilter *ToolFilter `json:"tool_filter,omitempty" yaml:"tool_filter,omitempty"`
}

// Transport identifies which transport adapter should serve a config.
type Transport string

const (
	// TransportUnknown means the config specifies no usable transport.
	TransportUnknown Transport = ""
	// TransportStdio is the local-subprocess transport.
	TransportStdio Transport = "stdio"
	// TransportSSE is the legacy HTTP+SSE transport (MCP 2024-11-05 spec).
	TransportSSE Transport = "sse"
	// TransportStreamableHTTP is the Streamable HTTP transport
	// (MCP 2025-03-26 spec). A single POST endpoint that returns either
	// application/json or text/event-stream.
	TransportStreamableHTTP Transport = "streamable_http"
)

// Transport returns the resolved transport. An explicit TransportName field
// wins; otherwise URL → TransportSSE (back-compat), Command → TransportStdio.
// Pointer receiver to avoid copying the (~120-byte) struct.
func (c *ServerConfig) Transport() Transport {
	if c.TransportName != "" {
		return c.TransportName
	}
	if c.URL != "" {
		return TransportSSE
	}
	if c.Command != "" {
		return TransportStdio
	}
	return TransportUnknown
}

// Registry interface defines the MCP server registry operations
type Registry interface {
	// RegisterServer adds a new MCP server configuration
	RegisterServer(config ServerConfig) error

	// UnregisterServer closes the client (if any) and removes the server
	// from the registry. Unknown names are no-ops.
	UnregisterServer(name string) error

	// GetClient returns an active client for the given server name
	GetClient(ctx context.Context, serverName string) (Client, error)

	// GetClientForTool returns the client that provides the specified tool
	GetClientForTool(ctx context.Context, toolName string) (Client, error)

	// ListServers returns all registered server names
	ListServers() []string

	// ListAllTools returns all tools from all connected servers
	ListAllTools(ctx context.Context) (map[string][]Tool, error)

	// GetServerConfig returns the configuration for a registered server.
	GetServerConfig(serverName string) (ServerConfig, bool)

	// Close shuts down all MCP servers and connections
	Close() error
}
