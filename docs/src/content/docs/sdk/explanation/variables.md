---
title: How Variables Resolve
description: Where variables apply, when they are read, and which source wins.
sidebar:
  order: 4
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/events/bus.go
    - runtime/events/types.go
    - runtime/pipeline/stage/errors.go
    - runtime/pipeline/stage/stages_memory.go
    - runtime/pipeline/stage/stages_utilities.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/session/duplex_session.go
    - sdk/session_vars.go
---

Where do variables apply, when does the SDK read them, and which source wins when several define the same name?

## Scope

Variables substitute into your pack's `system_template` and nowhere else. The SDK sends message text exactly as written, so a `{{name}}` in a message stays a placeholder, whether you put it there or a user typed it. To template your own message text, interpolate it before calling `Send`.

The render happens once per turn. Anything the prompt depends on must be known before the model runs, so a value your tool handler computes mid-turn appears in the next turn, not that one.

## Sources and precedence

```go
conv, _ := sdk.Open("./support.pack.json", "support",
    sdk.WithVariables(map[string]string{"tier": "standard"}),
    sdk.WithVariableProvider(myClock),
)

conv.SetVar("customer_name", "Alice")

resp, _ := conv.Send(ctx, "", sdk.WithJSONInput(map[string]any{"tier": "gold"}))
```

Later rows win:

| Source | Evaluated |
|---|---|
| Template defaults from the pack | Every turn |
| `variables.Provider` | Every turn, in registration order |
| Session variables: pack variable defaults, `WithVariables`, `SetVar` / `SetVars` | Read every turn |
| `WithJSONInput` binding | That `Send` only |

`Open` seeds the session variables with the pack's variable defaults, then `WithVariables` values. `WithVariables` wins over a pack default. Because the session variables rank above providers, a provider cannot override a name that `WithVariables`, a pack default or `SetVar` has already set. A provider supplies names the session does not hold.

A `SetVar` value persists until you change it and is re-read each turn, so changing it between sends changes the next prompt.

### Duplex conversations render once

A duplex conversation (`OpenDuplex`) has no per-turn render. The system prompt is rendered once, when the first input starts the session, and the provider session is created with that text as its system instruction for the life of the session. Set variables before the first `SendChunk` or `SendText`. A `SetVar` made after that is stored (`GetVar` returns it) but never reaches the provider, and the SDK logs a warning the first time it happens. The same applies to `variables.Provider` values, which are read once at session start.

## Unresolved placeholders fail the turn

A placeholder with no value fails the send. Nothing reaches the model.

```text
stage 'template' (transform) failed: system prompt has unresolved variables: support: unresolved template placeholders: [{{tier}}] (variables available: customer_name, locale)
```

The message lists variable names, never values. To detect this failure in code, use `errors.Is(err, stage.ErrTemplateUnresolved)` (package `runtime/pipeline/stage`).

## Seeing the rendered prompt

```go
bus := events.NewEventBus()
bus.Subscribe(events.EventTemplateRendered, func(e *events.Event) {
    if d, ok := e.Data.(*events.TemplateRenderedData); ok {
        fmt.Println(d.SystemPrompt)     // what the model receives
        fmt.Println(d.VariablesUsed)
        fmt.Println(d.UnusedVariables)  // set but never referenced: usually a typo
    }
})

conv, _ := sdk.Open("./support.pack.json", "support", sdk.WithEventBus(bus))
```

## Retrieved context

Ambient grounding writes `{{memory_context}}` as an ordinary variable: resolved before the render, once per turn, empty when a turn retrieves nothing. See [Memory and Grounding](/sdk/how-to/conversations/use-memory/).

## See also

- [Manage Variables](/sdk/how-to/conversations/manage-state/): the API, task by task
- [Tutorial 9: Variable Providers](/sdk/tutorials/09-variable-providers/): writing a provider
