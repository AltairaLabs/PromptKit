# Tool Support Analysis - SDK vs Arena

## Current State

### SDK Tool Support
- ❌ **NOT WORKING** - Tools are not functional in SDK
- Has a simplified `ToolRegistry` (`sdk/tools.go`)
- Only stores `ToolFunc` (simple Go functions)
- Does NOT support:
  - Tool descriptors (schemas, metadata)
  - Tool executors (mock, HTTP, MCP)
  - Tool validation
  - Tool modes (mock/live/mcp)
- Passes `nil` to `ProviderMiddleware` for tool registry

### Arena Tool Support
- ✅ **WORKING** - Tools fully functional
- Uses runtime's `tools.Registry` (`runtime/tools/registry.go`)
- Supports complete tool ecosystem:
  - **ToolDescriptors**: Name, description, input/output schemas
  - **Multiple executors**: Mock (static/scripted), HTTP, MCP
  - **Schema validation**: JSON Schema validation for args and results
  - **Tool modes**: `mock`, `live`, `mcp`
  - **File loading**: JSON and YAML tool descriptors
  - **Directory scanning**: Load all tools from a directory

## Key Differences

| Feature | SDK ToolRegistry | Runtime tools.Registry |
|---------|------------------|------------------------|
| **Type** | `map[string]ToolFunc` | `map[string]*ToolDescriptor` |
| **Executors** | None | Mock, HTTP, MCP |
| **Validation** | None | JSON Schema validation |
| **Schemas** | None | Input/Output schemas |
| **Tool Modes** | None | mock, live, mcp |
| **File Loading** | None | JSON, YAML, K8s manifests |
| **MCP Support** | No | Yes |

## Why SDK Tools Don't Work

### 1. Type Incompatibility
```go
// SDK (conversation.go:393)
pipelineMiddleware = append(pipelineMiddleware, middleware.ProviderMiddleware(
    c.manager.provider,
    nil, // ❌ Pass nil - tools disabled
    toolPolicy,
    providerConfig,
))
```

The `ProviderMiddleware` expects `*tools.Registry` but SDK has `*ToolRegistry`:

```go
// runtime/pipeline/middleware/provider.go
func ProviderMiddleware(
    provider providers.Provider,
    toolRegistry *tools.Registry,  // ← Expects runtime's Registry
    toolPolicy *pipeline.ToolPolicy,
    config *ProviderMiddlewareConfig,
) pipeline.Middleware
```

### 2. Missing Tool Descriptors
SDK's `ToolFunc` is too simple:
```go
// SDK version (sdk/tools.go)
type ToolFunc func(ctx context.Context, args json.RawMessage) (interface{}, error)
```

Runtime needs full descriptors:
```go
// Runtime version (runtime/tools/tool.go)
type ToolDescriptor struct {
    Name         string          `json:"name"`
    Description  string          `json:"description"`
    InputSchema  json.RawMessage `json:"inputSchema"`
    OutputSchema json.RawMessage `json:"outputSchema"`
    Mode         string          `json:"mode"` // "mock", "live", "mcp"
    MockResponse json.RawMessage `json:"mockResponse,omitempty"`
    MockTemplate string          `json:"mockTemplate,omitempty"`
    Endpoint     string          `json:"endpoint,omitempty"`
    TimeoutMs    int             `json:"timeoutMs"`
}
```

### 3. Missing Execution Infrastructure
SDK doesn't have:
- Executors (how to actually run tools)
- Validation (schema checking)
- Mode handling (mock vs live vs MCP)
- MCP integration

## Solution Options

### Option 1: Use Runtime's tools.Registry Directly (RECOMMENDED) ✅

**Pros:**
- Reuses battle-tested runtime code
- Full feature parity with Arena
- Supports MCP, validation, all tool modes
- No code duplication

**Cons:**
- SDK users need to work with ToolDescriptors (more complex API)
- Breaking change for existing SDK users (if any)

**Implementation:**
```go
// sdk/conversation.go
type ConversationManager struct {
    packManager  *PackManager
    provider     providers.Provider
    stateStore   statestore.Store
    toolRegistry *tools.Registry  // ← Change from SDK's ToolRegistry
    // ...
}

// Pass it to middleware
pipelineMiddleware = append(pipelineMiddleware, middleware.ProviderMiddleware(
    c.manager.provider,
    c.manager.toolRegistry,  // ← Now works!
    toolPolicy,
    providerConfig,
))
```

### Option 2: Create Adapter/Bridge Pattern

**Pros:**
- Keep SDK's simple `ToolFunc` API
- No breaking changes
- Users don't need to learn ToolDescriptors

**Cons:**
- Adds complexity (adapter code)
- Limited functionality (no schemas, validation, MCP)
- Code duplication
- Still need runtime's Registry internally

**Implementation:**
Create an adapter that converts SDK's `ToolFunc` to runtime's `ToolDescriptor`:
```go
func (tr *ToolRegistry) ToRuntimeRegistry() *tools.Registry {
    runtimeRegistry := tools.NewRegistry()
    
    for name, fn := range tr.tools {
        // Create descriptor with generic schema
        descriptor := &tools.ToolDescriptor{
            Name: name,
            Description: "SDK tool: " + name,
            InputSchema: []byte(`{"type": "object"}`), // Generic
            OutputSchema: []byte(`{"type": "object"}`),
            Mode: "mock",
        }
        
        // Register custom executor that calls SDK's ToolFunc
        executor := NewSDKToolExecutor(fn)
        runtimeRegistry.RegisterExecutor(executor)
        runtimeRegistry.Register(descriptor)
    }
    
    return runtimeRegistry
}
```

### Option 3: Hybrid Approach

**Pros:**
- Simple API for basic use cases
- Advanced features for power users
- Gradual migration path

**Cons:**
- Two APIs to maintain
- Confusion about which to use
- Still need adapter code

**Implementation:**
```go
type ConversationManager struct {
    provider         providers.Provider
    simpleTools      *ToolRegistry        // SDK's simple API
    advancedTools    *tools.Registry      // Runtime's full API
    // ...
}

// When creating pipeline, merge both registries
func (c *Conversation) getToolRegistry() *tools.Registry {
    if c.manager.advancedTools != nil {
        return c.manager.advancedTools
    }
    if c.manager.simpleTools != nil {
        return c.manager.simpleTools.ToRuntimeRegistry()
    }
    return nil
}
```

## Recommended Approach: Option 1

**Use runtime's `tools.Registry` directly in the SDK.**

### Why?

1. **Simplicity**: One tool system, no duplication
2. **Feature complete**: MCP, validation, all executors
3. **Consistency**: SDK and Arena work the same way
4. **Maintainability**: Single codebase for tools
5. **Future-proof**: Built for extensibility

### Migration Path

Since SDK is new and likely has few/no users yet:

1. Remove `sdk/tools.go` (SDK's ToolRegistry)
2. Change `ConversationManager.toolRegistry` to `*tools.Registry`
3. Update `WithToolRegistry` option
4. Wire up the registry in pipeline middleware
5. Add helper methods for common patterns
6. Create examples showing tool usage

### User Experience Improvement

Add SDK convenience methods to make ToolDescriptors easier:

```go
// sdk/tools_helpers.go

// SimpleToolDescriptor creates a basic tool descriptor for Go functions
func SimpleToolDescriptor(name, description string, fn ToolExecutorFunc) *tools.ToolDescriptor {
    return &tools.ToolDescriptor{
        Name:        name,
        Description: description,
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "input": {"type": "string"}
            }
        }`),
        OutputSchema: json.RawMessage(`{
            "type": "object", 
            "properties": {
                "output": {"type": "string"}
            }
        }`),
        Mode:      "mock",
        TimeoutMs: 3000,
    }
}

// RegisterSimpleTool registers a Go function as a mock tool
func (cm *ConversationManager) RegisterSimpleTool(
    name, description string,
    fn func(ctx context.Context, input string) (string, error),
) error {
    // Wrap the simple function in a proper executor
    descriptor := SimpleToolDescriptor(name, description, nil)
    executor := NewSimpleToolExecutor(fn)
    
    cm.toolRegistry.Register(descriptor)
    cm.toolRegistry.RegisterExecutor(executor)
    return nil
}

// LoadToolsFromPack loads tools from a Pack
func (cm *ConversationManager) LoadToolsFromPack(pack *Pack) error {
    for _, toolData := range pack.Tools {
        if err := cm.toolRegistry.LoadToolFromBytes(toolData.Name, toolData.Data); err != nil {
            return err
        }
    }
    return nil
}
```

## Implementation Checklist

- [ ] Remove `sdk/tools.go` (old ToolRegistry)
- [ ] Update `ConversationManager.toolRegistry` type to `*tools.Registry`
- [ ] Update `WithToolRegistry` option to accept `*tools.Registry`
- [ ] Wire registry into `Send()` pipeline middleware
- [ ] Wire registry into `SendStream()` pipeline middleware
- [ ] Add SDK helper methods for common tool patterns
- [ ] Create tool usage example
- [ ] Update Pack to support tool loading
- [ ] Document tool usage in SDK README
- [ ] Add tests for tool execution

## Next Steps

1. Confirm this approach with team
2. Implement changes (estimated 2-3 hours)
3. Create comprehensive examples
4. Test with MCP tools
5. Document patterns and best practices
