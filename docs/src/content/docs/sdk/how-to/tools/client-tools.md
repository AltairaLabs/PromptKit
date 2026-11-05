---
title: Register Client-Side Tools
description: Run tools on the caller's device, answering a tool call with a handler or by suspending the turn
sidebar:
  order: 5
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/a2a/types.go
    - runtime/tools/registry.go
    - runtime/tools/types.go
    - sdk/client_tools.go
    - sdk/internal/pack/load.go
    - sdk/response.go
    - sdk/streaming.go
    - sdk/tool_descriptor_override.go
    - server/a2a/server.go
---

Run a tool on the client (GPS, camera, biometrics, local files) and return its result to the model.

## Prerequisites

- A pack that declares the tool and lists it in the prompt's `tools`.
- An open conversation (see [Register Tools](/sdk/how-to/tools/register-tools/)).

## Steps

### 1. Declare the tool in the pack

A pack describes a tool's interface: name, description and parameters.

```json
{
  "prompts": {
    "assistant": { "id": "assistant", "system_template": "...", "tools": ["get_location"] }
  },
  "tools": {
    "get_location": {
      "name": "get_location",
      "description": "Get the user's current GPS location",
      "parameters": { "type": "object", "properties": {} }
    }
  }
}
```

The pack loader registers every pack tool with mode `local` and does not read a `mode` or `client` key. Calls to a `local` tool go to `OnTool` handlers, not `OnClientTool` handlers.

### 2. Mark the tool as client-side

Set the descriptor's mode and consent details with `WithToolDescriptorOverride`:

```go
conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithToolDescriptorOverride("get_location", func(d *tools.ToolDescriptor) {
        d.Mode = "client"
        d.ClientConfig = &tools.ClientConfig{
            Consent:    &tools.ConsentConfig{Message: "This app wants to access your location"},
            Categories: []string{"location"},
        }
    }),
)
```

`tools` is `github.com/AltairaLabs/PromptKit/runtime/v2/tools`. The consent message reaches your code as `ConsentMsg`, and `Categories` as `Categories`, on the request and on pending calls. Consent is the host's job: the SDK does not ask for it. When the user declines, return an error from the handler, or call `RejectClientTool` for a deferred call. `client.consent.required` and `client.consent.decline_strategy` are deprecated, nothing reads them, and they will be removed in v3.

### 3. Answer the call

To answer with a handler, register it with `OnClientTool`. It runs when the model calls the tool, and the pipeline sends its result to the model:

```go
conv.OnClientTool("get_location", func(ctx context.Context, req sdk.ClientToolRequest) (any, error) {
    return map[string]any{"lat": 40.7128, "lon": -74.0060}, nil
})

resp, err := conv.Send(ctx, "Where am I?")
fmt.Println(resp.Text())
```

To answer later, register no handler. The turn suspends and `Send` returns the pending calls:

```go
resp, err := conv.Send(ctx, "Where am I?")

if resp.HasPendingClientTools() {
    for _, tool := range resp.ClientTools() {
        fmt.Printf("Tool: %s (consent: %s)\n", tool.ToolName, tool.ConsentMsg)

        // Fulfill the call, or reject it with
        // conv.RejectClientTool(ctx, tool.CallID, "User denied location access")
        err := conv.SendToolResult(ctx, tool.CallID, map[string]any{
            "lat": 40.7128,
            "lon": -74.0060,
        })
        if err != nil {
            log.Fatal(err)
        }
    }

    // Resume once every pending call has a result.
    resp, err = conv.Resume(ctx)
    fmt.Println(resp.Text())
}
```

### 4. Stream deferred calls

When streaming, a `ChunkClientTool` chunk carries the pending call. Answer it, then call `ResumeStream`:

```go
for chunk := range conv.Stream(ctx, "Where am I?") {
    if chunk.Type == sdk.ChunkClientTool {
        err := conv.SendToolResult(ctx, chunk.ClientTool.CallID, map[string]any{
            "lat": 40.7128,
            "lon": -74.0060,
        })
        if err != nil {
            log.Fatal(err)
        }
        for resumeChunk := range conv.ResumeStream(ctx) {
            fmt.Print(resumeChunk.Text)
        }
        break
    }
    fmt.Print(chunk.Text)
}
```

### 5. Return images or audio (optional)

Pass `[]types.ContentPart` to `SendToolResultMultimodal`:

```go
chartPNG, err := generateChart(tool.Args)
if err != nil {
    log.Fatal(err)
}
err = conv.SendToolResultMultimodal(ctx, tool.CallID, []types.ContentPart{
    types.NewTextPart("Monthly sales chart for Q1 2026"),
    types.NewImagePartFromData(base64.StdEncoding.EncodeToString(chartPNG), "image/png", nil),
})
```

A handler can return the same `[]types.ContentPart` directly. Any other return value is serialized as JSON text.

### 6. Validate deferred results (optional)

A result returned by an `OnClientTool` handler is checked against the tool's `output_schema` before the model sees it. A result passed to `SendToolResult` is not, unless the tool sets `client.validate_output`:

```yaml
tools:
  get_location:
    mode: client
    output_schema:
      type: object
      properties:
        city: { type: string }
      required: [city]
    client:
      validate_output: true
```

With it set, `Resume` checks each JSON result against the schema. A result that does not match reaches the model as a failed call, with the validation error in place of the result. Multimodal results sent with `SendToolResultMultimodal` are not checked.

## Serve client tools over A2A

An A2A server reports a suspended turn as an input-required task. Each pending call is a status message part whose metadata carries the call. The example uses A2A 0.3 names; a 1.0 caller sees `TASK_STATE_INPUT_REQUIRED`, `ROLE_AGENT` and the `SendMessage` method with the same metadata:

```json
{
  "status": {
    "state": "input-required",
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

The caller answers with `message/send` on the same task. The message carries the task's `taskId`, and each result is a data part whose metadata names the `tool_call_id`:

```json
{
  "method": "message/send",
  "params": {
    "message": {
      "kind": "message",
      "messageId": "msg-2",
      "taskId": "input-required-task-id",
      "role": "user",
      "parts": [
        {
          "kind": "data",
          "data": { "lat": 40.7128, "lon": -74.0060 },
          "metadata": { "tool_call_id": "call_abc123" }
        }
      ]
    }
  }
}
```

The task returns to `working` and the context comes from the task. A result in the part's metadata as `tool_result` takes precedence over `data`. A message without a `taskId` still resumes the conversation, under a new task, and leaves the input-required one behind.

To reject a call, set `"rejected": "reason"` in the part's metadata instead of a result.

## Verify

- Handler mode: the handler runs and `resp.Text()` holds an answer that uses its result.
- Deferred mode: `resp.HasPendingClientTools()` is `true` after `Send`, and `false` after `Resume`.

## Troubleshoot

| Symptom | Cause |
|---|---|
| The handler never runs and the model never sees the result | The tool still has mode `local`. Apply the override from step 2. |
| `no resolved tool results to resume with` | `Resume` ran before `SendToolResult`, `SendToolResultMultimodal` or `RejectClientTool`. |
| The model does not call the tool | The prompt's `tools` list does not include it. |

## Related

- [Register Tools](/sdk/how-to/tools/register-tools/)
- [Override capability tools](/sdk/how-to/tools/override-capability-tools/)
- [Send Messages](/sdk/how-to/conversations/send-messages/)
