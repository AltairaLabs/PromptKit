# Resilience Testing Example

This example demonstrates Dynamic Context Testing features (Phases 1-7) using three scenarios:

1. **Statistical Trials** (`statistical-trials`) — Runs the same conversation multiple times (`trials: 3`) to measure response consistency across repeated executions. Uses `pass_threshold` to allow partial failures.

2. **Perturbation Testing** (`perturbation-testing`) — Varies input parameters (`{city}` placeholder with multiple values) to verify the model produces consistent behavior regardless of input variations.

3. **Chaos Injection** (`chaos-injection`) — Injects tool failures mid-conversation to verify the agent degrades gracefully. The first turn establishes a baseline with working tools, then the second turn forces `lookup_order` to fail and asserts the agent handles it appropriately.

## Running

Build PromptArena first:

```bash
make build-arena
```

Then run the example:

```bash
cd examples/resilience-testing
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --mock-provider --mock-config mock-responses.yaml --ci --formats html,json
open out/report.html
```

No API keys are required — all scenarios use mock providers and tools.
