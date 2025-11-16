---
layout: default
title: Tools & MCP
parent: Runtime Reference
grand_parent: Runtime
nav_order: 4
---

# Tools & MCP Reference

Tool registry, function calling, and Model Context Protocol integration.

## Overview

PromptKit provides comprehensive tool/function calling support through two main systems:

- **Tool Registry**: Manages tool descriptors, validation, and execution routing
- **MCP Integration**: Connects to external Model Context Protocol servers for dynamic tools

Both systems work together to provide seamless tool execution in LLM conversations.

## Tool Registry

### Core Types

#### ToolDescriptor

```go
type ToolDescriptor struct {
    Name         string
    Description  string
    InputSchema  json.RawMessage  // JSON Schema Draft-07
    OutputSchema json.RawMessage  // JSON Schema Draft-07
    Mode         string           // "mock", "live", "mcp"
    TimeoutMs    int
    MockResult   json.RawMessage  // Static mock data
    MockTemplate string           // Template for dynamic mocks
    HTTPConfig   *HTTPConfig      // Live HTTP configuration
}
```

#### Registry

```go
type Registry struct {
    repository ToolRepository
    tools      map[string]*ToolDescriptor
    validator  *SchemaValidator
    executors  map[string]Executor
}
```

### Constructor Functions

#### NewRegistry

```go
func NewRegistry() *Registry
```

Creates registry without repository backend (in-memory only).

**Example**:
```go
registry := tools.NewRegistry()
```

#### NewRegistryWithRepository

```go
func NewRegistryWithRepository(repository ToolRepository) *Registry
```

Creates registry with persistent storage backend.

**Example**:
```go
repo := persistence.NewFileRepository("/tools")
registry := tools.NewRegistryWithRepository(repo)
```

### Registry Methods

#### Register

```go
func (r *Registry) Register(descriptor *ToolDescriptor) error
```

Registers a tool descriptor with validation.

**Example**:
```go
tool := &tools.ToolDescriptor{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "location": {"type": "string", "description": "City name"}
        },
        "required": ["location"]
    }`),
    OutputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "temperature": {"type": "number"},
            "conditions": {"type": "string"}
        }
    }`),
    Mode: "mock",
    MockResult: json.RawMessage(`{"temperature": 72, "conditions": "sunny"}`),
}

if err := registry.Register(tool); err != nil {
    log.Fatal(err)
}
```

#### Get

```go
func (r *Registry) Get(name string) *ToolDescriptor
```

Retrieves tool descriptor by name.

**Example**:
```go
tool := registry.Get("get_weather")
if tool == nil {
    log.Fatal("Tool not found")
}
```

#### GetToolsByNames

```go
func (r *Registry) GetToolsByNames(names []string) ([]*ToolDescriptor, error)
```

Retrieves multiple tool descriptors. Returns error if any tool not found.

**Example**:
```go
tools, err := registry.GetToolsByNames([]string{"get_weather", "search_web"})
if err != nil {
    log.Printf("Some tools not found: %v", err)
}
```

#### List

```go
func (r *Registry) List() []string
```

Returns all registered tool names.

**Example**:
```go
toolNames := registry.List()
fmt.Printf("Available tools: %v\n", toolNames)
```

#### Execute

```go
func (r *Registry) Execute(
    descriptor *ToolDescriptor,
    args json.RawMessage,
) (json.RawMessage, error)
```

Executes a tool with validated arguments.

**Example**:
```go
argsJSON := json.RawMessage(`{"location": "San Francisco"}`)
result, err := registry.Execute(tool, argsJSON)
if err != nil {
    log.Fatal(err)
}

var weather map[string]interface{}
json.Unmarshal(result, &weather)
fmt.Printf("Temperature: %.0f°F\n", weather["temperature"])
```

#### RegisterExecutor

```go
func (r *Registry) RegisterExecutor(executor Executor)
```

Registers a custom tool executor.

**Example**:
```go
// Custom executor
type CustomExecutor struct{}

func (e *CustomExecutor) Name() string {
    return "custom"
}

func (e *CustomExecutor) Execute(
    descriptor *tools.ToolDescriptor,
    args json.RawMessage,
) (json.RawMessage, error) {
    // Custom execution logic
    return json.RawMessage(`{"result": "success"}`), nil
}

registry.RegisterExecutor(&CustomExecutor{})
```

### Tool Executors

#### Built-in Executors

**MockStaticExecutor**:
```go
// Static mock responses
tool := &tools.ToolDescriptor{
    Name: "get_weather",
    Mode: "mock",
    MockResult: json.RawMessage(`{"temp": 72}`),
}
```

**MockScriptedExecutor**:
```go
// Template-based mock responses
tool := &tools.ToolDescriptor{
    Name: "greet_user",
    Mode: "mock",
    MockTemplate: `{"message": "Hello {{.name}}!"}`,
}
```

**RepositoryExecutor**:
```go
// Execute from persistent storage
executor := tools.NewRepositoryExecutor(repository)
registry.RegisterExecutor(executor)
```

**MCPExecutor**:
```go
// Execute via MCP servers
mcpRegistry := mcp.NewRegistry()
executor := tools.NewMCPExecutor(mcpRegistry)
registry.RegisterExecutor(executor)

tool := &tools.ToolDescriptor{
    Name: "read_file",
    Mode: "mcp",  // Routes to MCP executor
}
```

#### HTTP Executor

```go
// Live HTTP API calls
tool := &tools.ToolDescriptor{
    Name: "external_api",
    Mode: "live",
    HTTPConfig: &tools.HTTPConfig{
        URL:       "https://api.example.com/endpoint",
        Method:    "POST",
        TimeoutMs: 5000,
        Headers: map[string]string{
            "Authorization": "Bearer ${API_KEY}",
            "Content-Type":  "application/json",
        },
        Redact: []string{"password", "apiKey"},
    },
}
```

### Tool Validation

#### Input Validation

```go
// Validate arguments against input schema
validator := tools.NewSchemaValidator()
err := validator.ValidateArgs(tool, argsJSON)
if err != nil {
    log.Printf("Invalid arguments: %v", err)
}
```

#### Output Validation

```go
// Validate result against output schema
err := validator.ValidateResult(tool, resultJSON)
if err != nil {
    log.Printf("Invalid result: %v", err)
}
```

### Tool Policy

```go
type ToolPolicy struct {
    ToolChoice          string   // "auto", "required", "none", or specific tool
    MaxRounds           int      // Max tool execution rounds
    MaxToolCallsPerTurn int      // Max tools per LLM response
    Blocklist           []string // Blocked tool names
}

policy := &pipeline.ToolPolicy{
    ToolChoice:          "auto",
    MaxRounds:           5,
    MaxToolCallsPerTurn: 10,
    Blocklist:           []string{"dangerous_tool"},
}
```

## Model Context Protocol (MCP)

### Overview

MCP enables LLMs to interact with external systems through standardized JSON-RPC protocol over stdio.

**Supported Transports**:
- stdio (currently implemented)
- HTTP/SSE (planned)

**Standard MCP Servers**:
- `@modelcontextprotocol/server-filesystem`: File operations
- `@modelcontextprotocol/server-memory`: Key-value storage
- Custom servers: Database, API, system command execution

### MCP Registry

#### Core Types

```go
type RegistryImpl struct {
    servers   map[string]ServerConfig
    clients   map[string]Client
    toolIndex map[string]string  // tool name -> server name
}

type ServerConfig struct {
    Name    string
    Command string
    Args    []string
    Env     map[string]string
}
```

#### Constructor Functions

**NewRegistry**:
```go
func NewRegistry() *RegistryImpl
```

**NewRegistryWithServers**:
```go
func NewRegistryWithServers(serverConfigs []ServerConfigData) (*RegistryImpl, error)
```

**Example**:
```go
registry := mcp.NewRegistry()
defer registry.Close()
```

#### Registry Methods

**RegisterServer**:
```go
func (r *RegistryImpl) RegisterServer(config ServerConfig) error
```

Registers an MCP server configuration.

**Example**:
```go
err := registry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
})
if err != nil {
    log.Fatal(err)
}
```

**GetClient**:
```go
func (r *RegistryImpl) GetClient(
    ctx context.Context,
    serverName string,
) (Client, error)
```

Gets or creates a client for the specified server.

**Example**:
```go
client, err := registry.GetClient(ctx, "filesystem")
if err != nil {
    log.Fatal(err)
}

tools, err := client.ListTools(ctx)
if err != nil {
    log.Fatal(err)
}
```

**GetClientForTool**:
```go
func (r *RegistryImpl) GetClientForTool(
    ctx context.Context,
    toolName string,
) (Client, error)
```

Finds the client that provides a specific tool.

**Example**:
```go
client, err := registry.GetClientForTool(ctx, "read_file")
if err != nil {
    log.Fatal(err)
}
```

**ListAllTools**:
```go
func (r *RegistryImpl) ListAllTools(
    ctx context.Context,
) (map[string][]Tool, error)
```

Lists all tools from all registered servers.

**Example**:
```go
serverTools, err := registry.ListAllTools(ctx)
for serverName, tools := range serverTools {
    fmt.Printf("Server %s:\n", serverName)
    for _, tool := range tools {
        fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
    }
}
```

**GetToolSchema**:
```go
func (r *RegistryImpl) GetToolSchema(
    ctx context.Context,
    toolName string,
) (*Tool, error)
```

Retrieves the schema for a specific tool.

**Example**:
```go
schema, err := registry.GetToolSchema(ctx, "read_file")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Input schema: %s\n", schema.InputSchema)
```

### MCP Client

#### Client Interface

```go
type Client interface {
    Initialize(ctx context.Context) (*InitializeResponse, error)
    ListTools(ctx context.Context) ([]Tool, error)
    CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error)
    Close() error
    IsAlive() bool
}
```

#### Client Creation

```go
// Stdio client
config := mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
    Env:     map[string]string{"DEBUG": "1"},
}

options := mcp.ClientOptions{
    RequestTimeout:  30 * time.Second,
    MaxRetries:      3,
    RetryBackoff:    time.Second,
}

client := mcp.NewStdioClientWithOptions(config, options)
defer client.Close()

// Initialize connection
info, err := client.Initialize(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Server: %s v%s\n", info.ServerInfo.Name, info.ServerInfo.Version)
```

#### Tool Operations

**List Tools**:
```go
tools, err := client.ListTools(ctx)
if err != nil {
    log.Fatal(err)
}

for _, tool := range tools {
    fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
}
```

**Call Tool**:
```go
args := json.RawMessage(`{"path": "/data/file.txt"}`)
response, err := client.CallTool(ctx, "read_file", args)
if err != nil {
    log.Fatal(err)
}

// Process response content
for _, content := range response.Content {
    if content.Type == "text" {
        fmt.Println(content.Text)
    }
}
```

### MCP Tool Integration

#### Automatic Discovery

```go
// Create MCP registry
mcpRegistry := mcp.NewRegistry()
defer mcpRegistry.Close()

// Register servers
mcpRegistry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
})

// Create tool registry
toolRegistry := tools.NewRegistry()

// Register MCP executor
mcpExecutor := tools.NewMCPExecutor(mcpRegistry)
toolRegistry.RegisterExecutor(mcpExecutor)

// Discover and register MCP tools
ctx := context.Background()
serverTools, err := mcpRegistry.ListAllTools(ctx)
if err != nil {
    log.Fatal(err)
}

for serverName, mcpTools := range serverTools {
    for _, mcpTool := range mcpTools {
        // Register as tool descriptor
        tool := &tools.ToolDescriptor{
            Name:        mcpTool.Name,
            Description: mcpTool.Description,
            InputSchema: mcpTool.InputSchema,
            Mode:        "mcp",  // Routes to MCP executor
        }
        toolRegistry.Register(tool)
    }
}
```

#### Manual Integration

```go
// Define MCP tool manually
tool := &tools.ToolDescriptor{
    Name:        "read_file",
    Description: "Read file contents",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "path": {"type": "string"}
        },
        "required": ["path"]
    }`),
    Mode: "mcp",
}

// Execute via MCP
argsJSON := json.RawMessage(`{"path": "/data/file.txt"}`)
result, err := toolRegistry.Execute(tool, argsJSON)
```

## Examples

### Basic Tool Registration

```go
// Create registry
registry := tools.NewRegistry()

// Register mock tool
tool := &tools.ToolDescriptor{
    Name:        "get_temperature",
    Description: "Get current temperature",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "city": {"type": "string"}
        },
        "required": ["city"]
    }`),
    OutputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "temperature": {"type": "number"},
            "unit": {"type": "string"}
        }
    }`),
    Mode: "mock",
    MockResult: json.RawMessage(`{"temperature": 72, "unit": "F"}`),
}

registry.Register(tool)

// Execute
args := json.RawMessage(`{"city": "SF"}`)
result, err := registry.Execute(tool, args)
```

### MCP Filesystem Integration

```go
// Setup MCP registry
mcpRegistry := mcp.NewRegistry()
defer mcpRegistry.Close()

mcpRegistry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
})

// Setup tool registry with MCP executor
toolRegistry := tools.NewRegistry()
toolRegistry.RegisterExecutor(tools.NewMCPExecutor(mcpRegistry))

// Discover and register tools
ctx := context.Background()
serverTools, _ := mcpRegistry.ListAllTools(ctx)
for _, mcpTools := range serverTools {
    for _, mcpTool := range mcpTools {
        toolRegistry.Register(&tools.ToolDescriptor{
            Name:        mcpTool.Name,
            Description: mcpTool.Description,
            InputSchema: mcpTool.InputSchema,
            Mode:        "mcp",
        })
    }
}

// Use in pipeline
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, toolRegistry, &pipeline.ToolPolicy{
        ToolChoice: "auto",
    }, config),
)

result, _ := pipe.Execute(ctx, "user", "Read the contents of data.txt")
```

### Custom Tool Executor

```go
// Custom async executor with human approval
type ApprovalExecutor struct{}

func (e *ApprovalExecutor) Name() string {
    return "approval"
}

func (e *ApprovalExecutor) Execute(
    descriptor *tools.ToolDescriptor,
    args json.RawMessage,
) (json.RawMessage, error) {
    // Synchronous fallback
    result, err := e.ExecuteAsync(descriptor, args)
    if err != nil {
        return nil, err
    }
    if result.Status == tools.ToolStatusPending {
        return nil, fmt.Errorf("tool requires approval")
    }
    return result.Content, nil
}

func (e *ApprovalExecutor) ExecuteAsync(
    descriptor *tools.ToolDescriptor,
    args json.RawMessage,
) (*tools.ToolExecutionResult, error) {
    // Check if approval required
    if requiresApproval(descriptor, args) {
        return &tools.ToolExecutionResult{
            Status: tools.ToolStatusPending,
            PendingInfo: &tools.PendingToolInfo{
                Reason:   "requires_approval",
                Message:  "Manager approval required",
                ToolName: descriptor.Name,
                Args:     args,
            },
        }, nil
    }
    
    // Execute immediately
    result := executeAction(descriptor, args)
    return &tools.ToolExecutionResult{
        Status:  tools.ToolStatusComplete,
        Content: result,
    }, nil
}

// Register executor
registry.RegisterExecutor(&ApprovalExecutor{})
```

## Best Practices

### 1. Tool Validation

```go
// Always validate schemas during registration
err := registry.Register(tool)
if err != nil {
    log.Printf("Invalid tool schema: %v", err)
}
```

### 2. Error Handling

```go
result, err := registry.Execute(tool, args)
if err != nil {
    // Check for validation errors
    if validErr, ok := err.(*tools.ValidationError); ok {
        log.Printf("Validation failed at %s: %s", validErr.Path, validErr.Detail)
    }
    return err
}
```

### 3. Timeout Configuration

```go
// Set appropriate timeouts
tool.TimeoutMs = 5000  // 5 second timeout

// MCP client timeout
options := mcp.ClientOptions{
    RequestTimeout: 30 * time.Second,
}
```

### 4. Resource Cleanup

```go
// Always close MCP registries
defer mcpRegistry.Close()

// Close individual clients if needed
defer client.Close()
```

### 5. Tool Blocklisting

```go
policy := &pipeline.ToolPolicy{
    Blocklist: []string{
        "delete_database",
        "system_shutdown",
    },
}
```

## Performance Considerations

### Tool Execution Latency

- **Mock tools**: <1ms
- **Repository tools**: 1-5ms
- **HTTP tools**: 100-1000ms (network dependent)
- **MCP tools**: 10-100ms (process spawn + IPC)

### MCP Overhead

- **Server startup**: 100-500ms (first call only)
- **Tool discovery**: 50-200ms (cached after first call)
- **Tool execution**: 10-50ms base overhead + tool execution time

### Optimization Tips

1. **Cache tool discovery**:
```go
// Preload tools at startup
serverTools, _ := mcpRegistry.ListAllTools(ctx)
```

2. **Reuse MCP clients**:
```go
// Registry automatically reuses clients
client, _ := registry.GetClient(ctx, "filesystem")
```

3. **Parallel tool execution**:
```go
// Execute tools concurrently when possible
var wg sync.WaitGroup
for _, tool := range toolCalls {
    wg.Add(1)
    go func(t ToolCall) {
        defer wg.Done()
        executeToolAsync(t)
    }(tool)
}
wg.Wait()
```

## See Also

- [Pipeline Reference](pipeline.md) - Using tools in pipelines
- [Tools How-To](../how-to/implement-tools.md) - Tool implementation guide
- [MCP Tutorial](../tutorials/03-mcp-integration.md) - Step-by-step MCP setup
- [Tools Explanation](../explanation/tool-architecture.md) - Tool system architecture
