# HTTP Transforms Example

Customize HTTP tool behavior with argument transforms, request hooks, and response processing.

## What You'll Learn

- `WithTransform()` to map/normalize arguments before the HTTP request
- `WithPreRequest()` to inject headers, correlation IDs, or dynamic auth
- `WithPostProcess()` to transform responses after receiving
- `WithRedact()` to strip sensitive fields before sending to the LLM

## Prerequisites

- Go 1.21+
- OpenAI API key

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

### Argument Transform

Map LLM arguments to the format your API expects:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/users",
    sdktools.WithTransform(func(args map[string]any) (map[string]any, error) {
        name, _ := args["name"].(string)
        parts := strings.SplitN(name, " ", 2)
        return map[string]any{
            "first_name": parts[0],
            "last_name":  parts[1],
        }, nil
    }),
)
conv.OnTool("lookup_user", cfg.Handler())
```

### Pre-Request Hook

Modify the HTTP request before sending (auth, tracing, etc.):

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/orders",
    sdktools.WithPreRequest(func(req *http.Request) error {
        req.Header.Set("X-Correlation-ID", generateID())
        req.Header.Set("Authorization", "Bearer "+getToken())
        return nil
    }),
)
conv.OnToolCtx("get_orders", cfg.HandlerCtx())
```

### Response Post-Processing + Redaction

Strip sensitive fields and add computed values:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/customers",
    sdktools.WithRedact("ssn", "credit_card"),
    sdktools.WithPostProcess(func(resp []byte) ([]byte, error) {
        var data map[string]any
        json.Unmarshal(resp, &data)
        data["display_name"] = data["first_name"].(string) + " " + data["last_name"].(string)
        return json.Marshal(data)
    }),
)
conv.OnTool("get_customer", cfg.Handler())
```

## Processing Pipeline

For each tool call, the pipeline runs in order:

1. **Transform** - Normalize arguments (`WithTransform`)
2. **Pre-Request** - Modify HTTP request (`WithPreRequest`)
3. **Execute** - Send HTTP request
4. **Post-Process** - Transform response (`WithPostProcess`)
5. **Redact** - Strip sensitive fields (`WithRedact`)

## Key Concepts

1. **Argument Mapping** - Bridge the gap between LLM output and API input
2. **Dynamic Auth** - Inject tokens at request time (OAuth refresh, etc.)
3. **Field Redaction** - Prevent sensitive data from reaching the LLM
4. **Computed Fields** - Enrich responses before the LLM sees them

## Next Steps

- [MCP Tools Example](../mcp-tools/) - MCP server integration
- [A2A Agent Example](../a2a-agent/) - Remote agent integration with auth
- [SDK HTTP Auth Example](../sdk-http-auth/) - More auth patterns
