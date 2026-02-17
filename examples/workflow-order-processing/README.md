# Workflow Order Processing Example

This example demonstrates how to use PromptKit Arena to test a workflow-based order processing system. The workflow models an e-commerce order lifecycle with external orchestration for payment processing.

## Workflow

```
              PaymentReceived        ItemShipped          Delivered
new_order ──────────────→ payment ──────────→ fulfillment ─────────→ complete
```

**States:**
- `new_order` — Order placed, awaiting payment
- `payment` — Payment confirmed, awaiting fulfillment (external orchestration)
- `fulfillment` — Order shipped, awaiting delivery
- `complete` — Terminal state, order delivered

**Events:**
- `PaymentReceived` — Payment gateway confirms payment
- `ItemShipped` — Warehouse ships the order
- `Delivered` — Carrier confirms delivery

## External Orchestration

The `payment` state uses `"orchestration": "external"`, meaning transitions out of this state are driven by external callers (e.g., a payment gateway webhook) rather than the agent itself. This models real-world payment processing where the system waits for an external confirmation.

## Files

- `config.arena.yaml` — Arena configuration
- `order.pack.json` — Pack file with `prompts` and `workflow` sections
- `prompts/order-workflow.yaml` — PromptConfig for order processing
- `providers/mock-provider.yaml` — Mock provider for deterministic testing
- `mock-responses.yaml` — Mock responses keyed by scenario and turn
- `scenarios/order-flow.scenario.yaml` — Full order lifecycle with event steps
- `scenarios/validation-checks.scenario.yaml` — Content + workflow assertions combined

## Running

```bash
cd examples/workflow-order-processing
promptarena run -c config.arena.yaml
```

### Running a Specific Scenario

```bash
promptarena run -c config.arena.yaml --scenario order-flow
```

## Key Concepts Demonstrated

- **Event-driven state transitions** — Using `event` steps to simulate external triggers
- **External orchestration** — Payment state waits for external confirmation
- **Context carry-forward** — Conversation context flows between states
- **Combined assertions** — Content assertions (`content_includes`, `content_matches`) alongside workflow assertions (`state_is`, `transitioned_to`, `workflow_complete`)
