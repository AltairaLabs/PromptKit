---
title: Apply Governance Policy in Your Host
sidebar:
  order: 9
---

Enforce your own governance policy over a pack's declarations at each point where your host decides: admission, tool calls, model calls, validators, evals and workflow transitions.

PromptKit carries every declaration a pack makes and hands it to your code. It makes no policy decision itself and never reads `extensions`. Each seam below gives you a copy of the pack's own definition, so changing it does not change the loaded pack.

## Prerequisites

- A pack that declares governance (see [Governance](/concepts/governance/)).
- A conversation opened with `sdk.Open`, `sdk.OpenWorkflow` or a `PackTemplate`.

## 1. Admit or refuse the pack

Read the declarations before you serve traffic. `Conversation.Governance()` returns the declaration that applies to the conversation, resolved against the pack's when it is opened as an agent. `PackTemplate.Pack()` returns the whole pack.

```go
import (
    "errors"
    "slices"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

conv, err := sdk.Open("./support.pack.json", "billing")
if err != nil {
    return err // includes a pack whose governance references do not resolve
}
g := conv.Governance()
if g != nil && !slices.Contains(g.ApprovedEnvironments, "production") {
    return errors.New("pack is not cleared for production")
}
```

`sdk.Open` already fails when an obligation, review or control names something the pack does not declare. `sdk.ValidatePack` reports the same errors before you open the pack, plus warnings for undeclared vocabulary prefixes.

## 2. Decide tool calls

Your tool executor receives the tool's declaration on `ToolDescriptor.Declaration`: its `action_scope` and `extensions`.

```go
import (
    "context"
    "encoding/json"
    "errors"

    "github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

type policyExecutor struct{ next tools.Executor }

func (e *policyExecutor) Name() string { return e.next.Name() }

func (e *policyExecutor) Execute(
    ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
    if d.Declaration == nil {
        return nil, errors.New("undeclared tool")
    }
    if s := d.Declaration.ActionScope; s != nil && s.Reversibility == "irreversible" {
        return nil, errors.New("irreversible tools need approval")
    }
    return e.next.Execute(ctx, d, args)
}
```

Register it with `sdk.WithToolExecutor(name, executor)` or `Conversation.OnToolExecutor`.

To allow or deny every tool in one place, use a tool hook instead. `hooks.ToolRequest.Declaration` carries the same value the executor receives:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/hooks"

func (h *toolPolicy) BeforeExecution(ctx context.Context, req hooks.ToolRequest) hooks.Decision {
    if req.Declaration == nil {
        return hooks.Deny("undeclared tool " + req.Name)
    }
    if req.Declaration.Extensions["acme.example/approver-group"] != nil {
        return hooks.Deny("needs approval")
    }
    return hooks.Allow
}
```

Register it with `sdk.WithToolHook`.

## 3. Decide calls to other agents

When a pack's agent calls another of the pack's agents over A2A, your A2A executor (`sdk.WithA2AToolExecutor`) receives the callee's definition on `ToolDescriptor.Agent`. Read the callee's `extensions` and its `governance` there. A tool discovered from a remote agent card carries no `Agent`.

## 4. Decide model calls

A provider hook receives the prompt the model is being invoked for on `hooks.ProviderRequest.Prompt`. After a workflow moves to another state within a turn, it is that state's prompt.

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/hooks"

func (h *promptPolicy) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    if req.Prompt != nil && req.Prompt.Extensions["acme.example/region"] == "eu" && !h.euModel(req.Model) {
        return hooks.Deny("prompt is restricted to EU-hosted models")
    }
    return hooks.Allow
}
```

Register it with `sdk.WithProviderHook`.

## 5. Observe validators and evals

Every `validation.started`, `validation.passed` and `validation.failed` event carries the validator's declaration on `events.ValidationEventData.Validator`, including its `id` and `extensions`. Subscribe on the bus you pass to `sdk.WithEventBus`:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/events"

bus := events.NewEventBus()
bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
    if d, ok := e.Data.(*events.ValidationEventData); ok && d.Validator != nil {
        audit.Record(d.Validator.ID, d.Validator.Extensions)
    }
})
conv, err := sdk.Open("./support.pack.json", "billing", sdk.WithEventBus(bus))
```

The validator's handler never receives its declaration: a validator's `extensions` are never configuration.

An eval hook (`sdk.WithEvalHook`) receives the whole eval definition, `extensions` included, as the `def` argument of `OnEvalResult`.

## 6. Decide workflow transitions

Implement `workflow.TransitionAuthorizer` and install it with `sdk.WithTransitionAuthorizer`. PromptKit calls it before every transition, whether the model requested it or you fired the event with `WorkflowConversation.Transition`.

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/workflow"

type transitionPolicy struct{}

func (transitionPolicy) AuthorizeTransition(ctx context.Context, req workflow.TransitionRequest) error {
    if req.ToState != nil && req.ToState.Extensions["acme.example/requires-verified-caller"] == true {
        return errors.New("caller is not verified")
    }
    return nil
}

wc, err := sdk.OpenWorkflow("./support.pack.json",
    sdk.WithTransitionAuthorizer(transitionPolicy{}),
)
```

The request carries the state names, both states' declarations and the event.

- When the model requests a transition you refuse, it receives a tool result with status `transition_refused` and your error text as the reason. The workflow stays in its state and the model answers from it.
- When you refuse a transition you fired, `Transition` returns the error.

The authorizer runs while the workflow conversation holds its lock, so it must not call back into the conversation.

## Undeclared means nil

Every seam passes `nil` for something the pack does not declare: a capability tool such as `workflow__transition` or `memory__recall`, an MCP tool, a handler with no pack entry, a guardrail added with `sdk.WithGuardrail`. Decide in your policy what an undeclared tool or validator may do.

## Verify

Open the pack, send one message that calls a tool, and log what each seam received. A declared tool shows its `extensions`; an undeclared one shows `nil`.

## Related pages

- [Governance](/concepts/governance/) explains the declarations and how agent governance overrides the pack's.
- [Write Custom Hooks](/sdk/how-to/hooks/custom-hooks/) covers each hook type.
