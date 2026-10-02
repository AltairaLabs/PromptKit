package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ProtocolVersion is the newest MCP protocol revision the client speaks: the
// stateless revision it uses with servers that support it.
//
// ProtocolVersion and LegacyProtocolVersion are the claims the conformance
// checks grade against: the mirrored schemas in testdata/spec, the parity
// test, the generated docs and the official suite all follow them.
const ProtocolVersion = "2026-07-28"

// LegacyProtocolVersion is the newest handshake-era revision the client
// speaks, with servers that predate ProtocolVersion's stateless protocol.
// The client also accepts the earlier handshake revisions a server may
// choose (2025-06-18, 2025-03-26, 2024-11-05).
const LegacyProtocolVersion = "2025-11-25"

// methodNotificationsInitialized is the notification a client MUST send after
// a successful initialize response, before any other request.
const methodNotificationsInitialized = "notifications/initialized"

// msgInitializedNotifyFailed is logged when that notification cannot be sent.
const msgInitializedNotifyFailed = "MCP initialized notification failed, continuing"

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

// InitializeResponse represents the initialization response. For a modern
// (2026-07-28) server, which has no handshake, the client builds it from the
// server/discover result.
type InitializeResponse struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
	// Instructions is the server's guidance on how to use it.
	Instructions string `json:"instructions,omitempty"`
}

// Implementation describes client or server implementation details
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Title is a display name.
	Title string `json:"title,omitempty"`
	// Description is a human-readable summary (2025-11-25).
	Description string `json:"description,omitempty"`
	// WebsiteURL links to the implementation's site (2025-11-25).
	WebsiteURL string `json:"websiteUrl,omitempty"`
	// Icons are display icons (2025-11-25).
	Icons []Icon `json:"icons,omitempty"`
}

// Icon is a display icon for an implementation, tool or resource (2025-11-25).
type Icon struct {
	Src      string   `json:"src"`
	MimeType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	// Theme is "light" or "dark", the background the icon is designed for.
	Theme string `json:"theme,omitempty"`
}

// ClientCapabilities describes what the client supports
type ClientCapabilities struct {
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
	Sampling    *SamplingCapability    `json:"sampling,omitempty"`
	// Deprecated: logging is a server capability; MCP defines no client
	// "logging" capability. The client never sets this.
	Logging *LoggingCapability `json:"logging,omitempty"`
	// Extensions advertises optional protocol extensions, keyed by
	// identifier (2026-07-28). The client advertises none.
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

// ServerCapabilities describes what the server supports
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
	// Extensions lists the optional protocol extensions the server supports,
	// keyed by identifier (2026-07-28).
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
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

// ElicitationCapability indicates the client supports elicitation. An empty
// object means form mode only; Form and URL name the modes explicitly
// (2025-11-25).
type ElicitationCapability struct {
	Form *struct{} `json:"form,omitempty"`
	URL  *struct{} `json:"url,omitempty"`
}

// SamplingCapability indicates the client supports sampling
type SamplingCapability struct{}

// LoggingCapability is not an MCP client capability: logging is declared by
// servers (ServerCapabilities.logging), not clients.
//
// Deprecated: the client never sends it. Setting ClientCapabilities.Logging
// sends a field the MCP spec does not define for clients.
type LoggingCapability struct{}

// ToolsListRequest represents a request to list available tools
type ToolsListRequest struct {
	// Cursor requests the page after the one that returned it as NextCursor.
	Cursor string `json:"cursor,omitempty"`
}

// ToolsListResponse represents the response to a tools/list request
type ToolsListResponse struct {
	Tools []Tool `json:"tools"`
	// NextCursor is set when more tools follow; the client requests the next
	// page with it.
	NextCursor string `json:"nextCursor,omitempty"`
	// ResultType, TTLMs and CacheScope are set by 2026-07-28 servers.
	// TTLMs is how long the list may be cached; CacheScope is "public" or
	// "private". The client does not cache tool lists.
	ResultType string                     `json:"resultType,omitempty"`
	TTLMs      *int64                     `json:"ttlMs,omitempty"`
	CacheScope string                     `json:"cacheScope,omitempty"`
	Meta       map[string]json.RawMessage `json:"_meta,omitempty"`
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"` // JSON Schema for tool input
	// Title is a display name.
	Title string `json:"title,omitempty"`
	// OutputSchema is the JSON Schema the tool's structuredContent conforms to.
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	// Annotations are hints about the tool's behavior. They are not
	// guaranteed: a client MUST NOT trust them from an untrusted server.
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
	// Icons are display icons (2025-11-25).
	Icons []Icon `json:"icons,omitempty"`
	// Execution says whether the tool runs as a task (2025-11-25). The
	// client does not implement tasks, so it never lists a tool that
	// requires one.
	Execution *ToolExecution             `json:"execution,omitempty"`
	Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
}

// ToolExecution holds a tool's execution properties (2025-11-25).
type ToolExecution struct {
	// TaskSupport is "forbidden" (the default), "optional" or "required".
	TaskSupport string `json:"taskSupport,omitempty"`
}

// taskSupportRequired marks a tool that may only be called as a task.
const taskSupportRequired = "required"

// ToolAnnotations are hints about a tool's behavior (server/tools).
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// ToolCallRequest represents a request to execute a tool
type ToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// InputResponses answers a modern server's input_required result, keyed
	// as the server keyed its inputRequests (2026-07-28 MRTR).
	InputResponses map[string]json.RawMessage `json:"inputResponses,omitempty"`
	// RequestState echoes, unchanged, the requestState of the input_required
	// result being answered.
	RequestState json.RawMessage `json:"requestState,omitempty"`
}

// ToolCallResponse represents the response from a tool execution
type ToolCallResponse struct {
	Content []Content `json:"content"`
	// StructuredContent is the tool's structured result (MCP 2025-06-18).
	// Servers SHOULD mirror it as serialized JSON in Content, but are not
	// required to, so it is the authoritative result when present.
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
	// ResultType is "complete" from a 2026-07-28 server (absent before).
	ResultType string                     `json:"resultType,omitempty"`
	Meta       map[string]json.RawMessage `json:"_meta,omitempty"`
}

// HasStructuredContent reports whether the response carries a non-null
// structuredContent payload.
func (r *ToolCallResponse) HasStructuredContent() bool {
	return len(r.StructuredContent) > 0 && string(r.StructuredContent) != jsonNull
}

// Content types a ContentBlock can have.
const (
	ContentTypeText         = "text"
	ContentTypeImage        = "image"
	ContentTypeAudio        = "audio"
	ContentTypeResourceLink = "resource_link"
	ContentTypeResource     = "resource"
)

// Content is one content block of a tool result. It is a flattened union of
// the spec's text, image, audio, resource_link and embedded resource blocks;
// Type says which fields apply.
type Content struct {
	Type     string `json:"type"` // one of the ContentType constants
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // Base64 encoded data (image, audio)
	MimeType string `json:"mimeType,omitempty"` // MIME type for data
	URI      string `json:"uri,omitempty"`      // URI for resource_link
	// Name, Title, Description and Size describe a resource_link.
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Size        *int64 `json:"size,omitempty"`
	Icons       []Icon `json:"icons,omitempty"`
	// Resource is an embedded resource's contents.
	Resource    *ResourceContents          `json:"resource,omitempty"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

// ResourceContents is the content of an embedded resource: Text for a text
// resource, Blob (base64) for a binary one.
type ResourceContents struct {
	URI      string                     `json:"uri"`
	MimeType string                     `json:"mimeType,omitempty"`
	Text     string                     `json:"text,omitempty"`
	Blob     string                     `json:"blob,omitempty"`
	Meta     map[string]json.RawMessage `json:"_meta,omitempty"`
}

// Annotations tell a client how to use a content block: who it is for and
// how important it is.
type Annotations struct {
	Audience     []string `json:"audience,omitempty"`
	Priority     *float64 `json:"priority,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
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
//   - URL:     HTTP transport — Streamable HTTP, falling back to the
//     deprecated HTTP+SSE transport when the server does not host a
//     Streamable HTTP endpoint. Set TransportName to pin one.
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
	// URL is the URL of an HTTP MCP server. Without a TransportName the
	// registry uses Streamable HTTP, falling back to HTTP+SSE if the server
	// does not host a Streamable HTTP endpoint at it.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// Headers are sent on HTTP transports (both SSE and Streamable HTTP).
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// TransportName selects the transport adapter explicitly. When empty it
	// is inferred: URL → Streamable HTTP (with the HTTP+SSE fallback),
	// Command → stdio.
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
// wins; otherwise URL → TransportStreamableHTTP, Command → TransportStdio.
// For a URL with no TransportName the registry also falls back to HTTP+SSE
// when the server does not host a Streamable HTTP endpoint.
// Pointer receiver to avoid copying the (~120-byte) struct.
func (c *ServerConfig) Transport() Transport {
	if c.TransportName != "" {
		return c.TransportName
	}
	if c.URL != "" {
		return TransportStreamableHTTP
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

// DiscoverResult is a server's answer to server/discover: its supported
// versions, capabilities and identity (2026-07-28 server/discover).
type DiscoverResult struct {
	SupportedVersions []string                   `json:"supportedVersions"`
	Capabilities      ServerCapabilities         `json:"capabilities"`
	Instructions      string                     `json:"instructions,omitempty"`
	ResultType        string                     `json:"resultType"`
	TTLMs             *int64                     `json:"ttlMs"`
	CacheScope        string                     `json:"cacheScope"`
	Meta              map[string]json.RawMessage `json:"_meta,omitempty"`
}

// InputRequiredResult is a server's interim answer asking for input before
// it completes a request (2026-07-28 multi round-trip requests).
type InputRequiredResult struct {
	ResultType    string                     `json:"resultType"`
	InputRequests map[string]InputRequest    `json:"inputRequests,omitempty"`
	RequestState  json.RawMessage            `json:"requestState,omitempty"`
	Meta          map[string]json.RawMessage `json:"_meta,omitempty"`
}

// InputRequest is one request for input inside an InputRequiredResult.
type InputRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// jsonNull is the JSON null literal.
const jsonNull = "null"

// RPCError is a JSON-RPC error returned by an MCP server. Callers can
// errors.As a request error into it to read the code and data.
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("JSON-RPC error %d: %s (data: %s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}
