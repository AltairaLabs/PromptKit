---
title: How Variables Resolve
sidebar:
  order: 4
---
Understanding when your variables are read, which source wins, and why a placeholder sometimes survives into the prompt.

## One substitution, once per turn

A `{{name}}` placeholder can appear in two places: your pack's `system_template`, and the text of a message you send. Both are substituted at the same moment — after the SDK has gathered every variable it can, and before the model is called.

That happens once per turn, not once per message and not once per conversation. The practical consequence is that everything a turn's prompt depends on has to be knowable before the model runs. A value your tool handler computes halfway through a turn cannot appear in that turn's system prompt; it lands in the next one.

## The four sources

```go
// 1. Declared in the pack, as a variable's default
// 2. Fixed at Open
conv, _ := sdk.Open("./support.pack.json", "support",
    sdk.WithVariables(map[string]string{"tier": "standard"}),
    // 3. Computed per turn
    sdk.WithVariableProvider(myClock),
)

// 4. Set on the conversation, sticky from here on
conv.SetVar("customer_name", "Alice")

// 5. Bound to a single Send
resp, _ := conv.Send(ctx, "", sdk.WithJSONInput(map[string]any{"tier": "gold"}))
```

When the same name comes from more than one of them, the most specific declaration wins:

| Source | Evaluated | Beats |
|---|---|---|
| Pack defaults | Load time | — |
| `WithVariables` | `Open` | pack defaults |
| `variables.Provider` | Every turn | the above |
| `SetVar` / `SetVars` | Read every turn | the above |
| `WithJSONInput` binding | That `Send` only | everything |

The ordering follows one rule: a declaration made later, or scoped more narrowly, is the one you meant. A provider computing a default is outranked by an explicit `SetVar`; a sticky `SetVar` is outranked by a value bound to the single call in front of you.

`SetVar` is sticky rather than static — the value persists until you change it, and it is re-read on every turn, so changing it between sends changes the next prompt.

## What an unresolved placeholder does

If any placeholder in your system template has no value, rendering fails and the **raw template** is sent to the model instead. Not the partially-rendered version — the original, with every `{{...}}` intact.

This is the failure worth recognizing, because one missing variable takes the whole prompt down with it, and the model receives literal braces rather than an empty gap. It usually reads as the model ignoring its instructions.

```
ERROR Template rendering failed in pipeline  error="unresolved template placeholders: [{{tier}}]"
```

A model given a prompt full of raw placeholders will often tell you so, which is the fastest diagnosis available:

> I don't have the reference material loaded — the template `{{memory_context}}` wasn't filled in.

## Seeing what was actually rendered

The rendered prompt is on the event bus, which is more reliable than reasoning about which source should have won:

```go
bus := events.NewEventBus()
bus.Subscribe(events.EventTemplateRendered, func(e *events.Event) {
    if d, ok := e.Data.(*events.TemplateRenderedData); ok {
        fmt.Println(d.SystemPrompt)   // exactly what the model will see
        fmt.Println(d.VariablesUsed)  // and which values got used
        fmt.Println(d.UnusedVariables)
    }
})

conv, _ := sdk.Open("./support.pack.json", "support", sdk.WithEventBus(bus))
```

`UnusedVariables` is the useful half when a value seems ignored: a name you set that the template never references shows up there, which usually means a typo on one side or the other.

## Where retrieved context fits

Ambient grounding writes its result into `{{memory_context}}` as an ordinary variable, subject to everything above — it is resolved before the render, once per turn, and a turn that retrieves nothing renders it empty. See [Memory and Grounding](/sdk/how-to/conversations/use-memory/).

## See also

- [Manage Variables](/sdk/how-to/conversations/manage-state/) — the API, task by task
- [Tutorial 9: Variable Providers](/sdk/tutorials/09-variable-providers/) — writing a provider
- [Memory and Grounding](/sdk/how-to/conversations/use-memory/) — retrieval as a variable source
