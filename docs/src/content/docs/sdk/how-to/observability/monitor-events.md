---
title: Monitor events
description: How to observe tool calls, provider usage, guardrails and messages with the hooks package
sidebar:
  order: 5
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/events/bus.go
    - runtime/events/message_created_data.go
    - runtime/events/types.go
    - runtime/metrics/collector.go
    - runtime/pipeline/stage/stages_message_broadcast.go
    - sdk/conversation.go
    - sdk/hooks/events.go
    - sdk/options.go
    - sdk/sdk.go
---

Subscribe to the runtime events a conversation publishes, using the `hooks` package.

## Prerequisites

- A conversation from `sdk.Open`. Every conversation has an event bus; you do not configure one.
- These imports:

  ```go
  import (
      "github.com/AltairaLabs/PromptKit/runtime/v2/events"
      "github.com/AltairaLabs/PromptKit/sdk/v2/hooks"
  )
  ```

## Steps

1. Subscribe to one event type with `hooks.On`:

   ```go
   hooks.On(conv, events.EventProviderCallStarted, func(e *events.Event) {
       log.Printf("provider call started at %s", e.Timestamp.Format("15:04:05"))
   })
   ```

   To see every event, use `hooks.OnEvent`:

   ```go
   hooks.OnEvent(conv, func(e *events.Event) {
       log.Printf("[%s] %s", e.Timestamp.Format("15:04:05"), e.Type)
   })
   ```

   The [runtime events](/architecture/runtime-events/) page lists the event types and their payloads.

2. Track tool calls and token usage with the typed helpers:

   ```go
   hooks.OnToolCall(conv, func(name string, args map[string]any) {
       log.Printf("tool %s(%v)", name, args)
   })

   hooks.OnProviderCall(conv, func(model string, inputTokens, outputTokens int, cost float64) {
       log.Printf("%s: %d in, %d out, $%.4f", model, inputTokens, outputTokens, cost)
   })
   ```

   `OnProviderCall` fires on `provider.call.completed`. `hooks.OnPipelineComplete` reports the totals for a whole turn.

3. Catch guardrail violations. The payload of every validation event is `*events.ValidationEventData`:

   ```go
   hooks.On(conv, events.EventValidationFailed, func(e *events.Event) {
       data := e.Data.(*events.ValidationEventData)
       log.Printf("guardrail %s: score=%.2f enforced=%v violations=%v",
           data.ValidatorName, data.Score, data.Enforced, data.Violations)
   })
   ```

   `Enforced` is true when the guardrail modified the content. `hooks.OnValidationFailed` delivers only the validator name and error.

4. Watch the conversation as it happens. `message.created` publishes each complete message: the user turn, every tool-calling round and the final answer.

   ```go
   hooks.On(conv, events.EventMessageCreated, func(e *events.Event) {
       d, ok := e.Data.(*events.MessageCreatedData)
       if !ok {
           return
       }
       fmt.Printf("[%d] %s: %s\n", d.Index, d.Role, d.GetContent())
   })
   ```

   Read `GetContent()` rather than `Content`. A user message carries its text in `Parts` with `Content` empty, and an assistant reply carries it in `Content` with no `Parts`.

   `Index` is the message's position in the whole transcript and continues across turns. Replayed history is not published again, so each message arrives once.

5. Debug prompt rendering. `prompt.template.rendered` reports the final system prompt and which variables it used; `prompt.template.failed` reports unresolved placeholders:

   ```go
   hooks.On(conv, events.EventTemplateRendered, func(e *events.Event) {
       data := e.Data.(*events.TemplateRenderedData)
       log.Printf("prompt %s: %d variables, unused=%v",
           data.PromptHash[:8], len(data.VariablesUsed), data.UnusedVariables)
   })

   hooks.On(conv, events.EventTemplateFailed, func(e *events.Event) {
       data := e.Data.(*events.TemplateFailedData)
       log.Printf("template failed: %s unresolved=%v", data.Error, data.UnresolvedPlaceholders)
   })
   ```

6. To share one bus across conversations, create it and pass it to each:

   ```go
   bus := events.NewEventBus()
   conv1, _ := sdk.Open("./app.pack.json", "support", sdk.WithEventBus(bus))
   conv2, _ := sdk.Open("./app.pack.json", "sales", sdk.WithEventBus(bus))
   ```

   You close a bus you supply. A handler registered through `hooks` stops when its conversation and any forks sharing the bus have closed, so a long-lived shared bus does not collect handlers. To keep listening after the conversation closes, call `bus.Subscribe` or `bus.SubscribeAll` on the bus directly; each returns its own unsubscribe function.

7. To export Prometheus metrics, attach a collector with `sdk.WithMetrics`:

   ```go
   collector := metrics.NewCollector(metrics.CollectorOpts{
       Registerer: prometheus.NewRegistry(),
       Namespace:  "myapp",
   })

   conv, _ := sdk.Open("./app.pack.json", "chat", sdk.WithMetrics(collector, nil))
   ```

   `metrics` is `github.com/AltairaLabs/PromptKit/runtime/v2/metrics`. Instance labels, eval metrics and the HTTP endpoint are in [Export Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/).

## Verify

Register the handlers from steps 1 and 2, then send a message:

```go
resp, err := conv.Send(ctx, "Hello!")
```

Each handler logs a line for the turn. Handlers run asynchronously, so a short program should call `conv.Close()` before it exits: `Close` waits up to one second for the turn's pipeline events to reach subscribers.

## Troubleshoot

- A message is missing from a live view. The bus never blocks the pipeline, so it drops an event when its buffer or a handler's queue is full. Read the state store for a transcript that must be complete, or use [session recording](/sdk/examples/session-recording/) for a lossless record with binary content.
- A handler stops after a few slow calls. The bus disables a handler whose calls exceed the subscriber timeout three times. Hand slow work to another goroutine.
- A type assertion on `e.Data` panics. Each event type has its own payload type; use the checked form (`d, ok := e.Data.(*events.X)`) when you subscribe with `OnEvent`.

## Related

- [Runtime events](/architecture/runtime-events/)
- [Observability](/sdk/explanation/observability/)
- [Export Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/)
- [Metrics reference](/runtime/reference/metrics/)
