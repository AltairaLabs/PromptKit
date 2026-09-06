---
title: How Variables Resolve
sidebar:
  order: 4
---
Where variables apply, when they are read, and which source wins.

## Scope

Variables substitute into your pack's `system_template` and nowhere else. Message text is sent exactly as written — a `{{name}}` in a message stays a placeholder, whether you put it there or a user typed it. To template your own message text, interpolate it before calling `Send`.

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
| Pack defaults | Load time |
| `WithVariables` | `Open` |
| `variables.Provider` | Every turn |
| `SetVar` / `SetVars` | Read every turn |
| `WithJSONInput` binding | That `Send` only |

`SetVar` is sticky, not static: the value persists until changed and is re-read each turn, so changing it between sends changes the next prompt.

## Unresolved placeholders fail the turn

A placeholder with no value fails the send. Nothing reaches the model.

```
system prompt has unresolved variables: support: unresolved template
placeholders: [{{tier}}] (variables available: customer_name, locale)
```

The message lists variable names, never values.

## Seeing the rendered prompt

```go
bus := events.NewEventBus()
bus.Subscribe(events.EventTemplateRendered, func(e *events.Event) {
    if d, ok := e.Data.(*events.TemplateRenderedData); ok {
        fmt.Println(d.SystemPrompt)     // what the model receives
        fmt.Println(d.VariablesUsed)
        fmt.Println(d.UnusedVariables)  // set but never referenced — usually a typo
    }
})

conv, _ := sdk.Open("./support.pack.json", "support", sdk.WithEventBus(bus))
```

## Retrieved context

Ambient grounding writes `{{memory_context}}` as an ordinary variable: resolved before the render, once per turn, empty when a turn retrieves nothing. See [Memory and Grounding](/sdk/how-to/conversations/use-memory/).

## See also

- [Manage Variables](/sdk/how-to/conversations/manage-state/) — the API, task by task
- [Tutorial 9: Variable Providers](/sdk/tutorials/09-variable-providers/) — writing a provider
