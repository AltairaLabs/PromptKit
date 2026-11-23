---
title: Register tools
description: Give the model Go functions to call with OnTool, custom executors and human approval.
sidebar:
  order: 3
verified:
  commit: 871dab9b182c410568ed21bad890a9d87d74b2c2
  sources:
    - runtime/pipeline/stage/internal/provider/tool_call_rate.go
    - runtime/pipeline/types.go
    - sdk/a2a.go
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/tools/pending.go
    - sdk/tools/redis.go
    - sdk/tools/typed.go
    - sdk/turn_gate.go
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

A handler runs only for a tool the model may call in that round: a tool the prompt lists (a `ToolSelector` that leaves it out of the request does not change that), a capability tool such as `memory__recall`, or a tool an active skill grants, and on a realtime session a tool in `StreamingInputConfig.Tools`. A call to any other registered tool gets the result `Tool <name> was not run: it is not available this turn`, after the tool policy's `Blocklist` and the tool hooks, and is reported with `tool.call.started` and `tool.call.failed`; its handler does not run. The tool hooks run before this check, so a hook sees every call the model attempts, and a call a hook blocks gets the hook's message rather than this one. What the model may call is taken once at the start of each round, so a tool a skill grants in one round can be called from the next. A refused call does not count toward `MaxToolCallsPerTurn`, does not end the turn as `termination.tool_called`, and does not count toward excluding the tool after repeated rejections; repeating it identically gets the same refusal each time, never the repeated-call nudge, and does not use up the one nudge `MaxIdenticalToolCalls` gives a turn. A name no tool is registered under fails with `tool not found`, before any approval check. A provider that cannot take tools, such as a replay provider, is not checked; a provider that can is checked even when it was offered no tools, for example under `tool_choice: none`. This holds on text, VAD and realtime turns.

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

A handler runs inside the conversation's turn, so it must not start a turn on the same conversation. Pass the context an `OnToolCtx` handler receives to any call it makes on that conversation, and the call fails with `sdk.ErrReentrantTurn`. An `OnTool` handler has no context to pass, so such a call waits for the turn that is running the handler: neither finishes unless the call's own context ends.

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

An executor that implements `tools.CallExecutor` is given each call through `ExecuteCall`; otherwise one that implements `tools.MultimodalExecutor` is given it through `ExecuteMultimodal`. The content parts either returns, such as an image, go to the model with the result. An executor that implements `tools.AsyncCallExecutor` or `tools.AsyncToolExecutor` is given each call through `ExecuteCallAsync` or `ExecuteAsync`: the content parts of its result go to the model, and a `tools.ToolStatusPending` result holds the call (see [When someone other than the client approves](#when-someone-other-than-the-client-approves)). `ExecuteCallAsync` is used when the executor has it. `ExecuteAsync` takes no call, so an executor reached through it, even one that also implements `tools.CallExecutor`, does not see the caller; it reads the call ID with `tools.CallIDFromContext`. A `tools.ExecExecutor` implements `ExecuteAsync`, so a subprocess that answers `{"pending": ...}` holds the call rather than failing it.

`OnToolExecutor` needs a conversation. Constructors that open conversations internally, such as `sdk.A2AOpener` (one per context ID), never return one. Pass the executor as an option instead:

```go
opener := sdk.A2AOpener(packPath, promptName,
    sdk.WithToolExecutor("search", myExecutor),
    sdk.WithToolExecutor("fetch", myExecutor),
)
```

`WithToolExecutor` applies the executor when the conversation opens and works with any constructor that takes options. Use it when the tool path must be identical whichever protocol the caller arrives on: policy checks, credential injection, audit.

## 5. Require approval before a tool runs

The built-in approval flow is for the case where the person approving is the client of the conversation: the app shows them the pending call, they decide, and the same app continues the turn. It fits approvals that take seconds or minutes, in one process:

- Answers are held in the process's memory between `ResolveTool` and `Continue`. Call both in the same process, and continue before the process exits.
- The approved tool runs inside the `ResolveTool` call, with whatever identity that request carries.
- A held call in the pending store expires after `sdktools.DefaultPendingTTL` (5 minutes) unless the store is configured otherwise.

When someone other than the client approves, such as a reviewer, a policy service or another team, possibly days later and on another machine, have the host own the approval: see [When someone other than the client approves](#when-someone-other-than-the-client-approves).

`OnToolAsync` takes a check function and an execution function. When the check returns a non-empty `PendingResult`, the pipeline holds the call instead of executing it. This applies to `Send`, to duplex sessions built with `WithIngestion`, and to realtime duplex sessions.

A call the check lets through runs on the tool's handlers, by the same rule in text, VAD and realtime conversations: a handler registered with `OnToolCall`, `OnToolExecutor`, `OnToolCtx` or `OnToolHTTP` runs it, even one registered before `OnToolAsync`; otherwise the execution function does, since it replaces an `OnTool` handler. With `OnToolAsyncCall`, the execution function runs it.

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

The approved handler's result is checked against the tool's `output_schema`, as the result of a call the turn runs is. A result that does not match never reaches the model: the resolution's `Error` carries the validation error, and `Continue` gives the model that error as the call's result. A failed approved call, whether its handler returned an error or its result did not match, reaches the model as a failed call the turn runs does: `Tool execution failed: <error>`.

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

Held calls live in an in-process store by default. They do not survive a restart, and only the process that held them can resolve them. To keep held calls across a restart or redeploy, inject a durable store with `WithPendingStore`:

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

The store keeps the held calls, not the answers. An answer stays in the memory of the process that called `ResolveTool` until that process calls `Continue`. This makes the store a way to survive a restart, not a way for several processes to settle one turn between them.

While a call waits, close the conversation with `conv.Close()` and resume it with `sdk.Resume` when the decision arrives. A close ends a session, not the conversation: it runs the pack's `on_session_complete` evals. Call `conv.Complete()` instead of `Close()` once the conversation is finished, so its `on_conversation_complete` evals score it once rather than at every pause.

### When someone other than the client approves

When the approver is not the client, such as a reviewer, a policy service or another person, and the decision can take days and arrive at another instance, the host owns the approval. PromptKit holds the call and resumes the turn. The host does everything in between: it records the referral, collects decisions, runs the tool as the right identity, keeps the audit trail and handles expiry.

The host holds a call from the executor that runs the tool. An executor registered with `OnToolExecutor` or `sdk.WithToolExecutor`, or in the conversation's tool registry, that implements `tools.AsyncToolExecutor` can return `tools.ToolStatusPending` from `ExecuteAsync` instead of a result. The pipeline then suspends the turn without running the tool. The executor receives the tool-execution context, which carries what the caller put on the `Send` context, so it can decide by who is calling. The held call appears in `resp.ClientTools()`, not in the pending store.

The process that answers can be a different one from the process that held the call. Close the conversation once the turn suspends. When every decision is in, reopen it with `sdk.Resume` on the same state store, with the same executor registered. Give each held call its answer, then resume:

```go
conv, _ := sdk.Resume(convID, "./assist.pack.json", "assist", sdk.WithStateStore(store))
// register the same executor as when the call was held
for _, d := range decisions {
    switch {
    case d.Approved:
        conv.SendToolResult(ctx, d.CallID, d.Result) // the host ran the tool
    default:
        conv.RejectClientTool(ctx, d.CallID, d.Reason)
    }
}
resp, err := conv.Resume(ctx) // one model request with every answer
```

- Answer every held call of the round before `Resume`. A `Resume` with calls still unanswered carries on without them. A later answer for one of them returns `sdk.ErrToolCallNotAwaited`.
- A large result, such as an image or a document, goes in as a part that refers to stored media, with `SendToolResultMultimodal` and a `types.New<Kind>PartFromStorageRef` part. The provider resolves the reference when it calls the model (see `WithMediaStorage`).
- Keep the conversation's state longer than any approval can take: the Redis state store's default TTL is 24 hours, and `statestore.WithTTL` changes it.
- The identity the resumed turn's tool calls act for is whatever the host puts on the `Resume` context, as it is for `Send`.

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
