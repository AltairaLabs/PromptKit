---
title: Conversation
sidebar:
  order: 2
---
The main type for interacting with LLMs in SDK.

## Creating a Conversation

```go
conv, err := sdk.Open("./pack.json", "promptName")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

## Methods

### Send

Send a message and wait for response.

```go
func (c *Conversation) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error)
```

**Example:**
```go
resp, err := conv.Send(ctx, "Hello!")
if err != nil {
    return err
}
fmt.Println(resp.Text())
```

### Stream

Stream a response in real-time.

```go
func (c *Conversation) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk
```

**Example:**
```go
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

### SetVar / GetVar

Manage template variables.

```go
func (c *Conversation) SetVar(name, value string)
func (c *Conversation) GetVar(name string) (string, bool)
func (c *Conversation) SetVars(vars map[string]any)
```

**Example:**
```go
conv.SetVar("user_name", "Alice")
name, ok := conv.GetVar("user_name")
```

### OnTool / OnToolCtx

Register tool handlers.

```go
func (c *Conversation) OnTool(name string, handler ToolHandler)
func (c *Conversation) OnToolCtx(name string, handler ToolHandlerCtx)
func (c *Conversation) OnTools(handlers map[string]ToolHandler)
```

**Example:**
```go
conv.OnTool("get_time", func(args map[string]any) (any, error) {
    return time.Now().Format(time.RFC3339), nil
})
```

### OnToolAsync

Register tool with approval workflow.

```go
func (c *Conversation) OnToolAsync(
    name string,
    check func(map[string]any) tools.PendingResult,
    execute ToolHandler,
)
```

**Example:**
```go
conv.OnToolAsync("process_refund",
    func(args map[string]any) tools.PendingResult {
        if args["amount"].(float64) > 100 {
            return tools.PendingResult{
                Reason: "high_value",
                Message: "Requires approval",
            }
        }
        return tools.PendingResult{}
    },
    func(args map[string]any) (any, error) {
        return map[string]any{"status": "done"}, nil
    },
)
```

### OnToolHTTP

Register HTTP-based tool.

```go
func (c *Conversation) OnToolHTTP(name string, config *tools.HTTPToolConfig)
```

### Event Hooks

Use the `hooks` package to subscribe to events.

```go
hooks.On(conv, events.EventType, func(e *events.Event) { ... })
hooks.OnEvent(conv, func(e *events.Event) { ... })
hooks.OnToolCall(conv, func(name string, args map[string]any) { ... })
hooks.OnProviderCall(conv, func(e *events.Event) { ... })
```

### ResolveTool / RejectTool

Handle pending approvals.

```go
func (c *Conversation) ResolveTool(id string) (*ToolResult, error)
func (c *Conversation) RejectTool(id string, reason string) (*ToolResult, error)
```

### Messages

Get conversation history.

```go
func (c *Conversation) Messages(ctx context.Context) []types.Message
```

### Clear

Clear conversation history.

```go
func (c *Conversation) Clear() error
```

### Fork

Create an isolated copy.

```go
func (c *Conversation) Fork() *Conversation
```

### Close

Close and clean up.

```go
func (c *Conversation) Close() error
```

### ID

Get conversation identifier.

```go
func (c *Conversation) ID() string
```

## Thread Safety

All methods are thread-safe for concurrent use.

## See Also

- [Response](response)
- [Reference Index](index)
