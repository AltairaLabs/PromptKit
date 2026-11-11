---
title: Register tools
description: Give the model Go functions to call with OnTool, custom executors and human approval.
sidebar:
  order: 3
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/pipeline/stage/tool_call_rate.go
    - runtime/pipeline/types.go
    - sdk/a2a.go
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/tools/pending.go
    - sdk/tools/redis.go
    - sdk/tools/typed.go
---
Register Go handlers for the tools your pack declares, so the model can call them during a conversation.

## Prerequisites

- A conversation from `sdk.Open`.
- Each tool declared in the pack and listed in the prompt's `tools`.

## 1. Declare the tool in the pack

```json
{
  "tools": {
    "get_time": {
      "name": "get_time",
      "description": "Get the current time",
      "parameters": {
        "type": "object",
        "properties": {
          "timezone": {
            "type": "string",
            "description": "Timezone (e.g., UTC, EST)"
          }
        }
      }
    }
  },
  "prompts": {
    "assistant": {
      "tools": ["get_time"]
    }
  }
}
```

## 2. Register a handler

A handler takes the model's arguments and returns any JSON-serializable value or an error:

```go
conv.OnTool("get_time", func(args map[string]any) (any, error) {
    tz := "UTC"
    if t, ok := args["timezone"].(string); ok {
        tz = t
    }
    return map[string]any{"time": time.Now().Format("15:04:05"), "timezone": tz}, nil
})
```

JSON numbers arrive as `float64`:

```go
query, _ := args["query"].(string)
limit, _ := args["limit"].(float64)
exact, _ := args["exact"].(bool)
```

A struct, map or slice return value is serialized to JSON for the model. A returned error goes to the model as the tool result, so it can explain the failure to the user:

```go
return nil, fmt.Errorf("user not found: %s", userID)
```

Register several handlers at once with `OnTools`:

```go
conv.OnTools(map[string]sdk.ToolHandler{
    "get_time":    getTime,
    "get_weather": getWeather,
})
```

## 3. Use the request context

`OnToolCtx` passes the pipeline context for cancellation, deadlines and tracing:

```go
conv.OnToolCtx("fetch_data", func(ctx context.Context, args map[string]any) (any, error) {
    url, _ := args["url"].(string)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    return string(body), nil
})
```

## 4. Call an HTTP API or a custom executor

Bind a tool to an HTTP endpoint without writing a handler:

```go
import sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"

conv.OnToolHTTP("create_ticket", sdktools.NewHTTPToolConfig(
    "https://api.example.com/tickets",
    sdktools.WithMethod("POST"),
))
```

See [HTTP tools](/sdk/how-to/tools/http-tools/) for authentication and reshaping.

Register a runtime `tools.Executor` directly with `OnToolExecutor`:

```go
conv.OnToolExecutor("custom_tool", &MyCustomExecutor{})
```

`OnToolExecutor` needs a conversation. Constructors that open conversations internally, such as `sdk.A2AOpener` (one per context ID), never return one. Pass the executor as an option instead:

```go
opener := sdk.A2AOpener(packPath, promptName,
    sdk.WithToolExecutor("search", myExecutor),
    sdk.WithToolExecutor("fetch", myExecutor),
)
```

`WithToolExecutor` applies the executor when the conversation opens and works with any constructor that takes options. Use it when the tool path must be identical whichever protocol the caller arrives on: policy checks, credential injection, audit.

## 5. Require approval before a tool runs

`OnToolAsync` takes a check function and an execution function. When the check returns a non-empty `PendingResult`, the pipeline holds the call instead of executing it. This applies to `Send`, to duplex sessions built with `WithIngestion`, and to realtime duplex sessions.

```go
conv.OnToolAsync("process_refund",
    func(args map[string]any) sdktools.PendingResult {
        amount, _ := args["amount"].(float64)
        if amount > 1000 {
            return sdktools.PendingResult{
                Reason:  "high_value_refund",
                Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
            }
        }
        return sdktools.PendingResult{} // proceed immediately
    },
    func(args map[string]any) (any, error) {
        return refundAPI.Process(args)
    },
)

resp, _ := conv.Send(ctx, "Process refund for order #123")
for _, pending := range resp.PendingTools() {
    conv.ResolveTool(ctx, pending.ID) // or conv.RejectTool(ctx, pending.ID, "reason")
}
resp, _ = conv.Continue(ctx)
```

Settle every held call before you continue. `conv.Continue(ctx)` gives the model every result of the round in one request and returns its reply. A `Continue` with calls still unanswered carries on without them, and the conversation then stops waiting on those calls.

The conversation does not wait on a call whose turn was continued or abandoned before the call was settled: `Continue` carried on without it, or `Send` started a new turn. `ResolveTool`, `ResolveToolWithArgs` and `RejectTool` return `sdk.ErrToolCallNotAwaited` for it and remove it from the store, and `ResolveTool` does not run its handler. `SendToolResult`, `SendToolResultMultimodal` and `FailClientTool` refuse such a call the same way.

When the check needs the caller's context (an identity or claims the caller put on the `Send` context, or a deadline for a remote policy service), register it with `OnToolAsyncWithContext`. The check also receives the provider's call id:

```go
conv.OnToolAsyncWithContext("process_refund",
    func(ctx context.Context, callID string, args map[string]any) sdktools.PendingResult {
        if policy.NeedsApproval(ctx, args) {
            return sdktools.PendingResult{Reason: "policy", Message: "Refund needs a reviewer"}
        }
        return sdktools.PendingResult{}
    },
    refundHandler,
)
```

If the pending store cannot record a hold, for example because Redis is down, the call fails closed. It does not run, the model is told the call needs approval and the hold could not be recorded, and the store's error is logged. The model is not given the store's error, which can name internal hosts.

### Supply your own approval gate

To decide every tool call yourself, for MCP and pack tools as well as `OnToolAsync` ones, pass a gate with `WithApprovalGate`. It runs before the `OnToolAsync` checks on `Send`, `Stream`, `Continue` and `Resume`. `OpenDuplex` refuses it with `sdk.ErrApprovalGateUnary`, because a duplex session gives the host no way to answer a call the gate holds. The gate receives the tool-execution context, the call id, the tool name and the arguments:

```go
conv, _ := sdk.Open("./assist.pack.json", "assist",
    sdk.WithApprovalGate(func(ctx context.Context, callID, name string, args map[string]any) (*tools.PendingToolInfo, error) {
        decision, err := policy.Decide(ctx, name, args)
        if err != nil {
            return nil, err // refuse: the call does not run, and the model is told why
        }
        if decision.Refer {
            referrals.Record(ctx, callID, name, args) // the host owns the hold
            return &tools.PendingToolInfo{Reason: "referral", ToolName: name}, nil
        }
        return nil, nil // no decision: the OnToolAsync check, if any, decides
    }),
)
```

A call the gate holds is the host's to answer. It appears in `resp.ClientTools()`, not in the pending store. Answer it with `SendToolResult` or `RejectClientTool`, then call `conv.Resume(ctx)`. Return an error rather than `nil` when the gate cannot record a hold, so the call fails closed.

To approve with edits, pass overrides that shallow-merge over the arguments the model proposed:

```go
conv.ResolveToolWithArgs(ctx, pending.ID, map[string]any{"amount": 500})
```

An approval counts toward `ToolPolicy.MaxCallsPerMinute` (see `WithToolPolicy`). When the tool has already reached the limit, the handler does not run and the model receives the limit error as the call's result.

To approve from a web service, resolve the call in a handler:

```go
http.HandleFunc("/approve", func(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Query().Get("id")
    if _, err := conv.ResolveTool(r.Context(), id); err != nil {
        http.Error(w, err.Error(), http.StatusConflict)
    }
})
```

### Persist approvals across restarts and instances

Held calls live in an in-process store by default. They do not survive a restart, and only the process that held them can resolve them. For a reviewer approving from another service, or approvals that outlive a redeploy, inject a durable store with `WithPendingStore`:

```go
import (
    "github.com/redis/go-redis/v9"
    sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

store := sdktools.NewRedisPendingStore(
    redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
)
conv, _ := sdk.Open("./assist.pack.json", "assist", sdk.WithPendingStore(store))
```

- A persisted call carries no Go closure. A process that resolves a call it did not create must register the same `OnToolAsync` handlers first. The runtime looks the handler up by tool name at resolve time.
- `ResolveTool` and `RejectTool` claim the call atomically (Redis `GETDEL`), so two instances cannot resolve one call twice. The loser gets `sdktools.ErrPendingAlreadyResolved`.

`conv.PendingTools(ctx)` lists the conversation's held calls from the store, including ones that survived a restart.

While a call waits, close the conversation with `conv.Close()` and resume it with `sdk.Resume` when the decision arrives. A close ends a session, not the conversation: it runs the pack's `on_session_complete` evals. Call `conv.Complete()` instead of `Close()` once the conversation is finished, so its `on_conversation_complete` evals score it once rather than at every pause.

## Verify

Send a message that makes the model call the tool:

```go
resp, err := conv.Send(ctx, "What time is it in UTC?")
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Text())
```

The reply uses the value your handler returned.

## Related

- [HTTP tools](/sdk/how-to/tools/http-tools/): builder API for HTTP tool handlers
- [Exec tools](/sdk/how-to/tools/exec-tools/): bind tools to external subprocesses
- [Client-side tools](/sdk/how-to/tools/client-tools/): tools that run on the caller's device
- [Tutorial 3: tools](/sdk/tutorials/03-tool-integration/): step-by-step walkthrough
