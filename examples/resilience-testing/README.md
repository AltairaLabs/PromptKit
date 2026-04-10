# Resilience Testing Example

Comprehensive example demonstrating 25+ assertion types. See the full
[Arena assertions reference](https://promptkit.altairalabs.ai/arena/reference/assertions/)
for the complete list of supported assertion types.

## Scenarios

| Scenario | What it tests |
|----------|--------------|
| **content-validation** | `min_length`, `max_length`, `content_includes`, `content_includes_any`, `content_excludes`, `content_matches`, `guardrail_triggered` |
| **tool-pattern-validation** | `tools_called`, `tools_not_called`, `tool_calls_with_args`, `tool_call_sequence`, `tool_no_repeat`, `tool_anti_pattern`, `tool_call_count`, `no_tool_errors` |
| **cost-and-guardrails** | `cost_budget`, `tool_call_count`, `no_tool_errors`, `guardrail_triggered` |
| **multi-turn-consistency** | Cross-turn `tool_call_sequence`, `tool_no_repeat`, `tool_anti_pattern`, `cost_budget` |
| **negative-testing** | `tools_not_called`, `content_excludes`, negative assertions |
| **tool-chain-and-efficiency** | `tool_call_chain`, `tool_efficiency`, multi-tool dependency chains |
| **behavioral-testing** | `outcome_equivalent`, `directional`, `invariant_fields_preserved` |
| **statistical-trials** | `trials: 3`, `pass_threshold` per assertion, flakiness detection |
| **perturbation-invariance** | `perturbations` with 3 variants (CheckList INV pattern) |

## Running

```bash
make build-arena
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --ci --formats html,json \
  -c examples/resilience-testing/config.arena.yaml \
  -o examples/resilience-testing/out
open examples/resilience-testing/out/report.html
```

No API keys required — all scenarios use mock providers and tools.
