---
title: Client-Side Tools
sidebar:
  order: 5
---
Tools that execute on the client rather than the server -- GPS, camera, biometrics, local file access, etc. Defined with `mode: client` in your tool YAML.

## Define a Client Tool

```yaml
tools:
  get_location:
    name: get_location
    description: Get the user's current GPS location
    mode: client
    client:
      consent:
        required: true
        message: "This app wants to access your location"
        decline_strategy: reject
    parameters:
      type: object
      properties: {}
```

## Synchronous Handler (OnClientTool)

Register a handler that runs immediately when the LLM invokes the tool:

```go
conv.OnClientTool("get_location", func(ctx context.Context, req sdk.ClientToolRequest) (any, error) {
    return map[string]any{"lat": 40.7128, "lon": -74.0060}, nil
})

resp, _ := conv.Send(ctx, "Where am I?")
fmt.Println(resp.Text()) // "You're in New York City!"
```

The pipeline handles the tool call automatically -- the LLM receives the result and continues generating.

## Deferred Mode

When no handler is registered, the pipeline suspends and returns pending tool calls to the caller:

```go
resp, _ := conv.Send(ctx, "Where am I?")

if resp.HasPendingClientTools() {
    for _, tool := range resp.ClientTools() {
        fmt.Printf("Tool: %s (consent: %s)\n", tool.ToolName, tool.ConsentMsg)

        // Fulfill the tool call
        conv.SendToolResult(ctx, tool.CallID, map[string]any{
            "lat": 40.7128,
            "lon": -74.0060,
        })

        // Or reject it
        // conv.RejectClientTool(ctx, tool.CallID, "User denied location access")
    }

    // Resume to get the final response
    resp, _ = conv.Resume(ctx)
    fmt.Println(resp.Text())
}
```

## Streaming Deferred Mode

Use `ResumeStream` after fulfilling tool calls during streaming:

```go
for chunk := range conv.Stream(ctx, "Where am I?") {
    if chunk.Type == sdk.ChunkClientTool {
        // Fulfill the tool call
        conv.SendToolResult(ctx, chunk.ClientTool.CallID, map[string]any{
            "lat": 40.7128,
            "lon": -74.0060,
        })

        // Resume streaming for the final response
        for resumeChunk := range conv.ResumeStream(ctx) {
            fmt.Print(resumeChunk.Text)
        }
        break
    }
    fmt.Print(chunk.Text)
}
```

## Consent Configuration

Control consent prompts in the tool YAML:

| Field | Description |
|-------|-------------|
| `client.consent.required` | If `true`, consent metadata is surfaced before execution |
| `client.consent.message` | Message to display to the user |
| `client.consent.decline_strategy` | What happens if declined: `reject` (return error) or `skip` (omit tool result) |

## A2A Integration

When serving via an A2A server, client tool suspension surfaces as an `input_required` task state. Tool metadata appears in the status message parts:

```json
{
  "status": {
    "state": "input_required",
    "message": {
      "role": "agent",
      "parts": [
        {
          "text": "Client tool required: get_location",
          "metadata": {
            "tool_call_id": "call_abc123",
            "tool_name": "get_location",
            "tool_args": { "accuracy": "high" },
            "consent_message": "This app wants to access your location"
          }
        }
      ]
    }
  }
}
```

The A2A client sends tool results back via `message/send` with `tool_call_id` and `tool_result` in part metadata:

```json
{
  "method": "message/send",
  "params": {
    "message": {
      "contextId": "original-context-id",
      "role": "user",
      "parts": [
        {
          "metadata": {
            "tool_call_id": "call_abc123",
            "tool_result": { "lat": 40.7128, "lon": -74.0060 }
          }
        }
      ]
    }
  }
}
```

To reject a tool, use `"rejected": "reason"` instead of `"tool_result"`:

```json
{
  "metadata": {
    "tool_call_id": "call_abc123",
    "rejected": "User denied location access"
  }
}
```

## Multimodal Tool Results

Use `SendToolResultMultimodal()` to return rich content (images, audio, etc.) from client tools alongside text.

### Returning an Image with Text

```go
resp, _ := conv.Send(ctx, "Generate a chart of monthly sales")

if resp.HasPendingClientTools() {
    for _, tool := range resp.ClientTools() {
        // Generate the chart image
        chartPNG, _ := generateChart(tool.Arguments)

        // Return multimodal result with text and image
        conv.SendToolResultMultimodal(ctx, tool.CallID, []types.ContentPart{
            {Type: "text", Text: "Monthly sales chart for Q1 2026"},
            {
                Type: "image",
                ImageURL: &types.ImageURL{
                    URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(chartPNG),
                },
            },
        })
    }

    resp, _ = conv.Resume(ctx)
    fmt.Println(resp.Text()) // LLM describes the chart
}
```

### Synchronous Multimodal Handler

For synchronous handlers, return `[]types.ContentPart` directly:

```go
conv.OnClientTool("capture_photo", func(ctx context.Context, req sdk.ClientToolRequest) (any, error) {
    photoBytes, _ := capturePhoto()
    return []types.ContentPart{
        {Type: "text", Text: "Photo captured at entrance"},
        {
            Type: "image",
            ImageURL: &types.ImageURL{
                URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(photoBytes),
            },
        },
    }, nil
})
```

When the handler returns `[]types.ContentPart`, the SDK automatically constructs a multimodal `MessageToolResult`. For any other return type, the result is serialized as JSON text.

## See Also

- [Register Tools](/sdk/how-to/register-tools/)
- [Send Messages](/sdk/how-to/send-messages/)
