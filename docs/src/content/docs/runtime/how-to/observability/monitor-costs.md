---
title: Monitor costs
description: Read, aggregate, cap and export the token usage and cost of LLM calls.
sidebar:
  order: 7
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/metrics/collector.go
    - runtime/pipeline/stage/builder.go
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/providers/base/aggregate.go
    - runtime/providers/base/priceusage.go
    - runtime/providers/mock/mock.go
    - runtime/providers/openai/openai.go
    - runtime/providers/openai/pricing_table.go
    - runtime/providers/provider.go
    - runtime/telemetry/listener.go
    - runtime/types/message.go
    - sdk/options.go
    - sdk/response.go
---
Track the tokens and dollars each call uses, total them per session or user, and stop spending at a limit.

## Prerequisites

- A provider that reports pricing. See [How calls are priced](/concepts/providers/#how-calls-are-priced).
- Either an SDK conversation or a `stage.StreamPipeline`.

## 1. Read the cost of one call

An SDK `Response` exposes the cost of the turn:

```go
resp, err := conv.Send(ctx, "Explain AI")
if err != nil {
    log.Fatal(err)
}
log.Printf("tokens: %d in, %d out, %d total", resp.InputTokens(), resp.OutputTokens(), resp.TokensUsed())
log.Printf("cost: $%.6f", resp.Cost())
```

A pipeline run returns a `types.CostInfo` on the `ExecutionResult`:

```go
msg := types.Message{Role: "user"}
msg.AddTextPart("Explain AI")
result, err := pipe.ExecuteSync(ctx, stage.NewMessageElement(&msg))
if err != nil {
    log.Fatal(err)
}

ci := result.CostInfo
log.Printf("input:  %d tokens, $%.6f", ci.InputTokens, ci.InputCostUSD)
log.Printf("output: %d tokens, $%.6f", ci.OutputTokens, ci.OutputCostUSD)
log.Printf("cached: %d tokens, $%.6f", ci.CachedTokens, ci.CachedCostUSD)
log.Printf("total:  $%.6f", ci.TotalCost)
```

`TotalCost` equals `InputCostUSD + OutputCostUSD + CachedCostUSD`. `InputTokens` and `OutputTokens` add up every modality (text, audio, image, video on input; text, audio, image on output). `CachedTokens` counts cache reads. Cache writes and reasoning tokens appear only in `Quantities` and `Breakdown`.

## 2. Break the cost down by unit

`Breakdown` lists one line per provider, capability and unit:

```go
for _, li := range result.CostInfo.Breakdown {
    log.Printf("%s/%s %s: %.0f units, $%.6f", li.Provider, li.Capability, li.Unit, li.Quantity, li.USD)
}
```

## 3. Total the cost across calls

`base.AggregateCost` merges `CostInfo` values, sums quantities and breakdown lines, and re-derives the headline fields. Use it instead of adding fields by hand.

```go
import (
    "sync"

    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
    "github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type SessionCosts struct {
    mu       sync.Mutex
    totals   map[string]types.CostInfo
    requests map[string]int
}

func NewSessionCosts() *SessionCosts {
    return &SessionCosts{totals: map[string]types.CostInfo{}, requests: map[string]int{}}
}

func (sc *SessionCosts) Record(sessionID string, ci types.CostInfo) {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    prev := sc.totals[sessionID]
    sc.totals[sessionID] = base.AggregateCost(&prev, &ci)
    sc.requests[sessionID]++
}

func (sc *SessionCosts) Stats(sessionID string) (types.CostInfo, int) {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    return sc.totals[sessionID], sc.requests[sessionID]
}
```

Key the map by user ID instead of session ID for per-user totals.

## 4. Enforce a budget

Check the running total before each call and record the cost after it:

```go
type Budgets struct {
    mu     sync.Mutex
    limits map[string]float64 // USD per key; a key without a limit is unlimited
    spent  map[string]float64
}

func (b *Budgets) Allow(key string) bool {
    b.mu.Lock()
    defer b.mu.Unlock()
    limit, ok := b.limits[key]
    return !ok || b.spent[key] < limit
}

func (b *Budgets) Record(key string, usd float64) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.spent[key] += usd
}
```

```go
if !budgets.Allow(userID) {
    return fmt.Errorf("budget exceeded for %s", userID)
}
resp, err := conv.Send(ctx, text)
if err != nil {
    return err
}
budgets.Record(userID, resp.Cost())
```

The check runs before the call, so the call that crosses the limit still completes.

## 5. Export cost to your monitoring stack

Alerts and dashboards belong in your metrics backend rather than in process. Two exports carry cost:

- A `metrics.Collector` attached with `sdk.WithMetrics` increments a `provider_cost_total` counter per provider call, plus per-capability counters for TTS, STT, embeddings, image generation and inference. See [Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/).
- The OTel listener sets `promptkit.provider.cost` on each provider span and `promptkit.pipeline.cost` on the pipeline span. See [Export traces with OTLP](/runtime/how-to/observability/export-traces-otlp/).

## Verify

Run a few calls against `mock.NewProvider`. Each reports nonzero token counts and a nonzero `TotalCost`, and `SessionCosts.Stats` shows the total and the request count growing with each `Record`.

## Troubleshoot

- Cost is `0`: the model has no pricing and the runtime logged `cost: nonzero token unit has no pricing; cost understated` once per unit. Set the provider's `pricing`. A provider with no pricing descriptor records `0` without a warning.
- Totals differ from the provider bill: the runtime prices usage from a pricing descriptor, the provider's `pricing` config or built-in rates. Built-in rates are a fallback and can lag a vendor's price change. Set `pricing` when cost feeds billing.
- Spend is higher than expected: lower `MaxTokens` in `providers.ProviderDefaults`, trim conversation history, or choose a cheaper model.

## Related

- [Set up providers](/runtime/how-to/providers/setup-providers/)
- [Configure the pipeline](/runtime/how-to/pipeline/configure-pipeline/)
- [Handle errors](/runtime/how-to/pipeline/handle-errors/)
- [Providers reference](/runtime/reference/providers/)
- [Pipeline reference](/runtime/reference/pipeline/)
