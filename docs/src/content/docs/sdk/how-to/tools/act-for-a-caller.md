---
title: Act for a caller
description: Set who a conversation acts for with WithCaller and read it in tool handlers, executors, hooks, retrievers, memory stores, evals and workflow authorizers.
sidebar:
  order: 17
---
Tell a conversation who it acts for, so your tool code, hooks, retrieval, memory, evals and workflow policy can authorize on that identity.

## Prerequisites

- A pack with at least one tool, registered as in [Register tools](/sdk/how-to/tools/register-tools/).
- An identity your application already resolved, such as a user ID and the claims from a verified token.

## 1. Set the caller

Pass `sdk.WithCaller` when you open the conversation. `types.Caller` holds an `ID` and a `Claims` map; PromptKit never reads either.

```go
package main

import (
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
	conv, err := sdk.Open("./app.pack.json", "assistant",
		sdk.WithCaller(types.Caller{
			ID:     "user-42",
			Claims: map[string]any{"tenant": "acme", "role": "support"},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()
}
```

`WithCaller` works on every open path, including `OpenDuplex` and `OpenWorkflow`. A conversation has one caller. `Fork` copies it. To act for another caller, open another conversation.

A `WithCaller` passed in the options of `sdk.A2AOpener` makes every conversation the opener creates act for that one caller, whichever remote A2A client sent the request. To act for each remote client, see step 9, Serve A2A clients as themselves.

Keep `Claims` to JSON-encodable data when you configure exec hooks. PromptKit encodes the caller into the JSON on an exec hook's stdin, so a value `encoding/json` cannot encode, such as a func or a channel, makes an exec filter hook deny every call. Treat `Claims` as read-only in your code: parallel tool calls, forks and hooks share one map.

## 2. Read it in a tool handler

`OnToolCall` receives the whole call, caller included:

```go
conv.OnToolCall("get_balance", func(ctx context.Context, call sdk.ToolCall) (any, error) {
	return accounts.Balance(ctx, call.Caller.ID)
})
```

`OnTool` and `OnToolCtx` handlers do not receive it. Use `OnToolCall` for tools that act on behalf of the caller.

A client tool handler reads `ClientToolRequest.Caller`:

```go
conv.OnClientTool("get_location", func(ctx context.Context, req sdk.ClientToolRequest) (any, error) {
	return locations.For(req.Caller.ID), nil
})
```

## 3. Read it in an approval check and an approved tool call

`OnToolAsyncCall` takes a check and an execution handler that each receive the whole call, caller included. The check decides whether the call waits for approval:

```go
conv.OnToolAsyncCall("process_refund",
	func(ctx context.Context, call sdk.ToolCall) sdktools.PendingResult {
		if call.Caller.Claims["role"] != "manager" {
			return sdktools.PendingResult{Reason: "needs_manager", Message: "A manager must approve this refund"}
		}
		return sdktools.PendingResult{}
	},
	func(ctx context.Context, call sdk.ToolCall) (any, error) {
		return refunds.Process(ctx, call.Caller.ID, call.Args)
	})
```

When a person approves a held call with `ResolveTool`, the execution handler runs with the provider's call ID of the held call and the caller of the conversation that resolves it. If you resume a conversation to approve a call, for example from a durable `WithPendingStore`, open it with the original requester's `WithCaller`.

An approved held call always runs this handler, even if a later registration binds another handler to the same tool name.

## 4. Read it in an executor

An executor that implements `tools.CallExecutor` receives a `tools.ToolCall` with the caller on `Caller`. Implement `tools.AsyncCallExecutor` as well if the executor can return `tools.ToolStatusPending`:

```go
type accountExecutor struct{}

func (accountExecutor) Name() string { return "accounts" }

func (accountExecutor) Execute(
	ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	return nil, errors.New("accounts: needs a call")
}

func (accountExecutor) ExecuteCall(
	ctx context.Context, d *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	body, err := accounts.Lookup(ctx, call.Caller.ID, call.Args)
	return body, nil, err
}
```

Register it with `sdk.WithToolExecutor(name, executor)` or `conv.OnToolExecutor`. An executor you pass to `sdk.WithA2AToolExecutor` to govern remote A2A agent calls receives the caller the same way.

`tools.RepositoryToolExecutor`, which answers calls from a mock repository and falls back to the executor it wraps, hands a wrapped `CallExecutor` the whole call, caller included. It runs any other executor through `Execute`, and never suspends a call.

An executor that implements only `tools.AsyncToolExecutor` does not receive the caller. Implement `tools.AsyncCallExecutor` instead. An executor that implements `AsyncToolExecutor` and `CallExecutor` but not `AsyncCallExecutor` gets the caller on synchronous calls and none on asynchronous ones.

## 5. Read it in a hook

`hooks.ToolRequest.Caller` and `hooks.SessionEvent.Caller` carry the caller to tool hooks and session hooks:

```go
type tenantPolicy struct{}

func (tenantPolicy) Name() string { return "tenant-policy" }

func (tenantPolicy) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
	if req.Caller.Claims["tenant"] != "acme" {
		return hooks.Deny("tool not allowed for this tenant")
	}
	return hooks.Allow
}

func (tenantPolicy) AfterExecution(_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}
```

Register it with `sdk.WithToolHook(tenantPolicy{})`.

An exec hook receives the caller in the JSON on its stdin, as `"Caller": {"ID": "...", "Claims": {...}}` inside the request or event it handles.

`hooks.ProviderRequest.Caller` carries the caller to provider hooks, in `BeforeCall` and `AfterCall`, so a per-tenant guardrail can judge a turn by who it acts for. `guardrails.InputFunc` and `guardrails.OutputFunc` receive it on `hooks.InputRequest.Caller` and `hooks.OutputRequest.Caller`. A guardrail built from an eval handler passes it on as `evals.EvalContext.Caller`, and while streaming to a handler that implements `evals.StreamableRequestEvalHandler`, on `evals.PartialEvalRequest.Caller`.

A provider hook that also implements `hooks.ChunkRequestInterceptor` judges each streamed chunk with the caller. PromptKit calls its `InterceptChunk` in place of `OnChunk`:

```go
func (tenantOutputPolicy) InterceptChunk(_ context.Context, req hooks.ChunkRequest) hooks.Decision {
	if req.Caller.Claims["tenant"] == "acme" && strings.Contains(req.Chunk.Content, "internal") {
		return hooks.Deny("internal content not allowed for this tenant")
	}
	return hooks.Allow
}
```

A hook that implements only `hooks.ChunkInterceptor` keeps receiving `OnChunk`, without the caller.

Realtime duplex sessions (Gemini Live and OpenAI Realtime, `WithStreamingConfig`) run the same tool hooks, provider hooks and chunk interceptors as text turns, with the same caller, so the hooks above cover them too.

## 6. Read it in a workflow authorizer

`workflow.TransitionRequest.Caller` carries the caller to the `TransitionAuthorizer` you install with `sdk.WithTransitionAuthorizer`. It is set for transitions the model fires and for transitions your code fires:

```go
type transitionPolicy struct{}

func (transitionPolicy) AuthorizeTransition(_ context.Context, req workflow.TransitionRequest) error {
	if req.To == "approved" && req.Caller.Claims["role"] != "manager" {
		return errors.New("only managers approve")
	}
	return nil
}
```

## 7. Read it in a retriever and a memory store

A retriever you pass to `sdk.WithRetriever` or `sdk.WithMemoryRetriever` receives the caller when it implements `memory.ContextRequestRetriever`. PromptKit calls `RetrieveContextRequest` in place of `RetrieveContext`:

```go
type tenantRetriever struct{ index *search.Index }

func (r tenantRetriever) RetrieveContext(
	ctx context.Context, scope map[string]string, msgs []types.Message,
) ([]*memory.Memory, error) {
	return nil, errors.New("tenantRetriever: needs a caller")
}

func (r tenantRetriever) RetrieveContextRequest(
	ctx context.Context, req memory.ContextRequest,
) ([]*memory.Memory, error) {
	return r.index.Search(ctx, req.Caller.Claims["tenant"], req.Messages)
}
```

A memory extractor (`sdk.WithMemoryExtractor`) receives it the same way when it implements `memory.ContextRequestExtractor`.

A memory store you pass to `sdk.WithMemory` receives the caller of the model's memory tool calls on `memory.RetrieveOptions.Caller` (`memory__recall`) and `memory.ListOptions.Caller` (`memory__list`). Writes reach it with the caller only through two optional interfaces:

- `memory.OptionsSaver`: `memory__remember` and the memories the extractor produces are saved through `SaveWithOptions`, with the caller on `memory.SaveOptions.Caller`. A store with only `Save` does not receive it.
- `memory.ExtrasDeleter`: `memory__forget` deletes through `DeleteWithOptions`, with the caller on `memory.DeleteOptions.Caller`. A store with only `Delete` does not receive it.

A store that scopes reads by caller must implement both, or a caller's writes go unauthorized. When a store or retriever takes the caller anywhere but the store lacks either interface, PromptKit logs a warning naming what is missing when it registers the memory tools. `DeleteAll` never receives the caller; PromptKit does not call it.

## 8. Read it in an eval handler

`evals.EvalContext.Caller` carries the caller to the eval handlers a conversation runs, per turn and at session end. `sdk.Evaluate` passes `EvaluateOpts.Caller` the same way.

The built-in `tool_exec` eval runs its tool with the caller on the call, so a `CallExecutor` behind it authorizes the eval's call as it would the conversation's. An exec eval handler receives it in the JSON on its stdin, as `"caller": {"ID": "...", "Claims": {...}}` inside `context`.

## 9. Serve A2A clients as themselves

The A2A protocol carries no caller identity of its own: your authentication middleware puts it on the HTTP request. `sdk.A2ACallerOpener` opens each conversation for the caller of the request that opens its context. Serve it with `sdk.NewA2AServerFor`, which takes a function naming the caller of each request:

```go
callerOf := func(r *http.Request) types.Caller {
	id := auth.FromContext(r.Context())
	return types.Caller{ID: id.Subject, Claims: map[string]any{"tenant": id.Tenant}}
}

open, err := sdk.A2ACallerOpener("./app.pack.json", "assistant", callerOf)
if err != nil {
	log.Fatal(err)
}
srv := sdk.NewA2AServerFor(func(r *http.Request) string { return callerOf(r).ID }, open)
```

The server keeps one conversation per context ID, opened by the first message that names the context, and that conversation belongs to the caller who opened it. A message from another caller naming the context is refused with an invalid-params error, and a request the function gives no identity is refused as unauthorized. Tasks are scoped the same way: `GetTask`, `CancelTask`, `ListTasks` and `SubscribeToTask` see only the caller's own tasks, and another caller's task is reported as not found. An `a2aserver.WithTaskOwner` option replaces the function for task scoping, and the task store must implement `a2aserver.OwnedTaskStore`; the default in-memory store does. Opening a context does not hold up messages for other contexts.

`A2ACallerOpener` returns an error for a nil `callerOf`. The caller it reads replaces a `WithCaller` in its options. With `a2aserver.NewServerFor` you can open conversations your own way: its opener receives the request alongside the context ID.

## What PromptKit does with the caller

PromptKit puts the caller on the requests it hands your code and does nothing else with it. It does not persist it in the state store, and it does not write it to logs, events, spans or recordings. It does not travel on `context.Context`.

`WithCaller` differs from `WithUserID` and `WithSessionMetadata`, which PromptKit persists with the conversation. If you resume a conversation, set the caller again on the new open.

## Verify

Register a handler that returns `call.Caller.ID`, send a message that makes the model call the tool, and check that the tool result contains the ID you passed to `WithCaller`. A conversation opened without `WithCaller` hands your code the zero `types.Caller`; `Caller.IsZero()` reports it.

## Related

- [Register tools](/sdk/how-to/tools/register-tools/)
- [Apply a governance policy](/sdk/how-to/hooks/apply-governance-policy/)
- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)
- [`types.Caller` reference](/runtime/reference/types/)
