# Voice Provider Bake-Off Demo

Run the same scenario across multiple providers side by side. Arena fans out the scenario over every registered provider; the HTML report shows per-provider response, latency, and assertion outcomes on one axis.

## What it tests

One three-turn support call, registered twice with different providers (`mock-fast`, `mock-slow`). Both run; both produce per-turn `latency_budget` and `max_length` outcomes. The report puts them side by side — same scenario, different providers, observable difference in response style, length, and timing.

## Running

```bash
cd examples/voice-bake-off
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

The report's run table groups by provider; expand each turn to see the response and the per-turn metrics from each provider.

Live dev loop:

```bash
../../bin/promptarena serve   # web UI with side-by-side timeline
../../bin/promptarena run --tui
```

## Adding real providers

The default config uses text mock providers so the demo runs keyless. To run the bake-off across real voice providers:

1. Add provider configs (`providers/openai-realtime.provider.yaml`, `providers/gemini-live.provider.yaml`, `providers/cartesia.provider.yaml`).
2. Register them in `config.arena.yaml` under `providers:`.
3. Add a `duplex:` block to `scenarios/standard-call.scenario.yaml` (see `examples/voice-refund-demo/scenarios/*.yaml` for the shape).
4. Run with provider keys in your environment.

The scenario stays the same; the I/O layer changes. The report's side-by-side view scales with the number of registered providers.

## Per-provider thresholds

If a provider has tighter or looser real-world limits than the rest, narrow the assertion with `when:`:

```yaml
- type: latency_budget
  params:
    max_ms: 800
  when:
    provider: openai-realtime
- type: latency_budget
  params:
    max_ms: 1500
  when:
    provider: gemini-live
```

## File layout

```
voice-bake-off/
├── README.md
├── config.arena.yaml              # both providers registered here
├── mock-responses-fast.yaml       # short, snappy responses
├── mock-responses-slow.yaml       # longer, more conversational
├── prompts/support-agent.yaml
├── providers/
│   ├── mock-fast.provider.yaml
│   └── mock-slow.provider.yaml
└── scenarios/standard-call.scenario.yaml
```
