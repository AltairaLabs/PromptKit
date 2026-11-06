---
title: Handle SDK errors
description: Tell a guardrail denial, a provider failure, a pack load failure and a tool failure apart with errors.As, and match the SDK's sentinel errors with errors.Is.
sidebar:
  order: 8
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/client_tools.go
    - sdk/conversation.go
    - sdk/errors.go
    - sdk/errors_wrap.go
    - sdk/evaluate.go
    - sdk/multi_agent_open.go
    - sdk/streaming.go
    - sdk/validate_pack.go
---

Branch on why a call failed: a hook denied the turn, the provider failed, the pack did not load, or a tool handler returned an error.

## Prerequisites

- A conversation that calls `Send` or `Stream` (see [Send messages](/sdk/how-to/conversations/send-messages/)).

## Steps

### 1. Match a typed error with errors.As

The SDK gives a failed turn one of two types and wraps the runtime error inside it, so `errors.As` still finds the runtime error.

| Type | Returned when | Useful fields |
|---|---|---|
| `*sdk.ValidationError` | A hook or guardrail denies the turn, or a chunk hook aborts the stream. | `ValidatorType`, `Message`, `Details` |
| `*sdk.ProviderError` | The provider answers with an error status, or the connection fails. | `Provider`, `StatusCode`, `Message`, `Cause` |

`Send`, `Stream` and their `Resume` and `Continue` counterparts apply this wrapping. Any other error comes back unchanged.

```go
resp, err := conv.Send(ctx, message)
if err != nil {
	var pErr *sdk.ProviderError
	if vErr, ok := sdk.AsValidationError(err); ok {
		log.Printf("denied by %s: %s", vErr.ValidatorType, vErr.Message)
	} else if errors.As(err, &pErr) {
		log.Printf("%s failed (%d): %s", pErr.Provider, pErr.StatusCode, pErr.Message)
	}
	return err
}
fmt.Println(resp.Text())
```

`ValidatorType` is the `validator_type` the guardrail names in its decision metadata, and the hook type when it names none. A stream abort reports `provider_chunk`.

`ProviderError.StatusCode` is zero for a connection failure. `Provider` falls back to the conversation's provider ID when the runtime error does not name one. `Cause` holds the runtime error, so `providers.IsTransient(err)` still decides whether a retry is sensible.

### 2. Handle load and tool errors

| Type | Returned by | Fields |
|---|---|---|
| `*sdk.PackError` | `Open`, `OpenDuplex`, `OpenWorkflow`, `OpenMultiAgent`, `ValidatePack` and `Evaluate`, when the pack is missing, unreadable, malformed or invalid. | `Path`, `Cause` |
| `*sdk.ToolError` | `ResolveTool` and `ResolveToolWithArgs`, as the `ToolResolution` error, when an approved tool's handler fails. | `ToolName`, `Cause` |

`errors.Is(err, sdk.ErrPackNotFound)` still matches a missing pack file through `PackError`.

When a tool handler fails during a turn, the model receives the handler's own error text. For a `*sdk.ToolError` it receives the `Cause` text alone, because it already knows which tool it called.

### 3. Match sentinels with errors.Is

| Sentinel | Meaning |
|---|---|
| `ErrConversationClosed` | `Send` or `Stream` on a closed conversation. |
| `ErrConversationNotFound` | `sdk.Resume` found no conversation with that ID. |
| `ErrNoStateStore` | `sdk.Resume` with no state store configured. |
| `ErrPromptNotFound` | The pack has no prompt with that name. |
| `ErrPackNotFound` | The pack file does not exist. |
| `ErrProviderNotDetected` | No provider API key was found in the environment. |
| `ErrToolNotRegistered` | The model called a tool that has no handler. |
| `ErrToolNotInPack` | A handler registered with `OnToolExecutor` is for a tool the pack does not define. |
| `ErrNoWorkflow` | `OpenWorkflow` on a pack without a workflow section. |
| `ErrWorkflowClosed` | `Send` or `Transition` on a closed `WorkflowConversation`. |
| `ErrWorkflowTerminal` | `Transition` from a terminal state. |
| `ErrMessageTooLarge` | A user message exceeds the configured maximum size. |

## Verify

Send a message that a guardrail denies, or point the provider at a bad key. The matching `errors.As` branch runs.

## Related

- [Handle pipeline errors](/runtime/how-to/pipeline/handle-errors/)
- [Write custom hooks](/sdk/how-to/hooks/custom-hooks/)
