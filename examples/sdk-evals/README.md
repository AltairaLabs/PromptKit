# SDK Evals Example

Demonstrates how to use PromptKit's eval framework with the SDK.

## What It Shows

- **Pack-level evals**: Eval definitions live in the pack file (`assistant.pack.json`) — no Go code needed to define them
- **InProcDispatcher**: Runs evals synchronously in-process (simplest pattern)
- **MetricCollector**: Accumulates eval results and exports Prometheus text format
- **Turn vs session evals**: Turn evals run after each `Send()`, session evals run on `Close()`

## Eval Definitions

The pack defines four evals:

| ID | Type | Trigger | What It Checks |
|----|------|---------|----------------|
| `json_format` | `json_valid` | `every_turn` | Response is valid JSON |
| `response_not_empty` | `contains` | `every_turn` | Response contains "response" field |
| `no_apologies` | `regex` | `every_turn` | No unnecessary "I'm sorry" patterns |
| `session_coverage` | `contains_any` | `on_session_complete` | At least one substantive answer in session |

## Running

```bash
export OPENAI_API_KEY=sk-...
go run ./examples/sdk-evals
```

## Expected Output

```
=== SDK Evals Example ===

Turn 1: What is the capital of France?
  Response: {"response": "The capital of France is Paris."}

Turn 2: What is 2 + 2?
  Response: {"response": "2 + 2 equals 4."}

Turn 3: List three programming languages.
  Response: {"response": "Go, Python, and TypeScript."}

=== Prometheus Metrics ===

# TYPE promptpack_json_format_valid boolean
promptpack_json_format_valid 1
# TYPE promptpack_no_unnecessary_apologies boolean
promptpack_no_unnecessary_apologies 1
# TYPE promptpack_response_has_content boolean
promptpack_response_has_content 1
# TYPE promptpack_session_has_substance boolean
promptpack_session_has_substance 1
```

## Key Concepts

### Dispatcher Patterns

This example uses `InProcDispatcher` (Pattern A). For production, consider:

- **EventDispatcher** (Pattern B): Publishes eval requests to an event bus for async worker processing
- **EventBusEvalListener** (Pattern C): Subscribes to `message.created` events and triggers evals automatically — no middleware needed

### MetricResultWriter

The `MetricResultWriter` only records results for evals that have a `metric` definition in the pack. Evals without metrics still run (for pass/fail reporting) but aren't tracked in Prometheus.

### Eval Resolution

If a prompt also defines evals, they are merged with pack evals:
- Prompt evals **override** pack evals with the same ID
- Pack-only and prompt-only evals are preserved

See [Eval Framework docs](https://promptkit.dev/arena/explanation/eval-framework/) for details.
