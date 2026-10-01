package mcp

// The transport-specific clients keep PromptKit's public constructors. Each
// is the go-sdk-backed client fixed to one transport; the registry picks the
// transport from a ServerConfig itself.

// StdioClient runs an MCP server as a subprocess and talks to it over stdin
// and stdout.
type StdioClient struct{ *sdkClient }

// NewStdioClient creates a stdio client with default options.
func NewStdioClient(config ServerConfig) *StdioClient {
	return NewStdioClientWithOptions(config, DefaultClientOptions())
}

// NewStdioClientWithOptions creates a stdio client with custom options.
func NewStdioClientWithOptions(config ServerConfig, options ClientOptions) *StdioClient {
	return &StdioClient{newSDKClient(config, options, TransportStdio)}
}

// StreamableClient talks to an MCP server over Streamable HTTP.
type StreamableClient struct{ *sdkClient }

// NewStreamableClient creates a Streamable HTTP client with default options.
func NewStreamableClient(config ServerConfig) *StreamableClient {
	return NewStreamableClientWithOptions(config, DefaultClientOptions())
}

// NewStreamableClientWithOptions creates a Streamable HTTP client with custom options.
func NewStreamableClientWithOptions(config ServerConfig, options ClientOptions) *StreamableClient {
	return &StreamableClient{newSDKClient(config, options, TransportStreamableHTTP)}
}

// SSEClient talks to an MCP server over the deprecated HTTP+SSE transport
// (MCP 2024-11-05). ServerConfig.URL is the SSE endpoint.
type SSEClient struct{ *sdkClient }

// NewSSEClient creates an HTTP+SSE client with default options.
func NewSSEClient(config ServerConfig) *SSEClient {
	return NewSSEClientWithOptions(config, DefaultClientOptions())
}

// NewSSEClientWithOptions creates an HTTP+SSE client with custom options.
func NewSSEClientWithOptions(config ServerConfig, options ClientOptions) *SSEClient {
	return &SSEClient{newSDKClient(config, options, TransportSSE)}
}
