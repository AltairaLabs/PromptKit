---
title: Act for a caller
description: Set who a conversation acts for with WithCaller and read it in tool handlers, executors, hooks and workflow authorizers.
sidebar:
  order: 17
---
Tell a conversation who it acts for, so your tool code, hooks and workflow policy can authorize on that identity.

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

A `WithCaller` passed in the options of `sdk.A2AOpener` makes every conversation the opener creates act for that one caller, whichever remote A2A client sent the request.

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

Realtime duplex sessions (Gemini Live and OpenAI Realtime, `WithStreamingConfig`) run no tool hooks. Executors in those sessions still receive the caller, so enforce policy there in the executor.

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
