# Codegen Eval — Methodology Spike

This directory hosts the v1 spike for the codegen evaluation methodology
described in `docs/local-backlog/CODEGEN_EVAL_METHODOLOGY_PROPOSAL.md`
(local-only). Goal: validate that the substrate produces a usable
signal before sinking effort into SWE-bench adaptation or expanding
the task suite.

## What's here

```
benchmarks/codegen-eval/
├── sonnet-baseline.arena.yaml       Bundle A — no skill
├── sonnet-disciplined.arena.yaml    Bundle D — with skill
├── scenarios/                       5 hand-rolled Go bugfix tasks
│   ├── go-add-bugfix                (easy, arithmetic)
│   ├── go-strings-reverse           (easy, encoding)
│   ├── go-fizzbuzz-order            (medium, control-flow)
│   ├── go-counter-race              (medium, concurrency)
│   └── go-binary-search             (hard, boundary)
├── packs/
│   └── codegen-agent.yaml           Shared bare prompt + tool palette
├── providers/
│   └── claude-sonnet.provider.yaml  claude-sonnet-4-6
└── skills/
    └── codegen-disciplined/         Read-before-Edit + verify-before-done
```

The two arena configs live at the spike root (not under `configs/`)
because arena's path-traversal protection rejects scenario/skill paths
that escape the config directory. Forward-only paths only.

The two bundles share everything except the skill. That's the v1
question: *does the discipline skill change pass-rate enough to
justify the extra tool calls?*

## Running

Requires Docker + `ANTHROPIC_API_KEY` exported (or in a `.env` the
arena binary can read). The `tool_exec` gate calls `run_tests` which
the codegen-sandbox returns isError=true on test failures — so a
single hidden-test failure flips the assertion to fail.

```bash
make -C ../.. build-arena
docker pull ghcr.io/altairalabs/codegen-sandbox:latest

# Bundle A — baseline
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run \
  --config sonnet-baseline.arena.yaml --ci --formats json,html

# Bundle D — disciplined
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run \
  --config sonnet-disciplined.arena.yaml --ci --formats json,html
```

For variance bands, set `spec.trials: 3` on each scenario before the
run (or leave at 1 for a smoke pass). Per-rep raw JSON files land in
`out/<bundle>/`; aggregate `pass_rate` and `flakiness_score` land in
`out/<bundle>/report-data.json[].trial_results`.

## Reporting

Stratified pass-rate query (works on either bundle's `report-data.json`):

```bash
jq '[.results[] | {
       diff: .Labels.difficulty,
       cat:  .Labels.category,
       passed: .ConversationAssertions.passed
     }] |
     group_by(.diff) |
     map({diff: .[0].diff,
          n: length,
          pass: ([.[] | select(.passed)] | length)})' \
  out/sonnet-baseline/report-data.json
```

Pareto cost vs pass-rate per bundle:

```bash
for b in sonnet-baseline sonnet-disciplined; do
  jq --arg b "$b" '
    [.results[] | {pass: .ConversationAssertions.passed, cost: .Cost.TotalCost}] |
    {bundle: $b,
     n: length,
     pass_rate: ([.[] | select(.pass)] | length) / length,
     total_cost: ([.[] | .cost] | add)}' \
    "out/$b/report-data.json"
done
```

## Cost ballpark

5 scenarios × 2 bundles × 3 reps = 30 sessions. Sonnet 4.6 at 0.003/1k
input + 0.015/1k output, with `max_tokens: 4096` and typical bugfix
sessions running ~5-10 turns: roughly $0.10 per session, ~$3 per
matrix sweep. Setting trials=1 for a first pass cuts that by 3×.

## What this spike will and won't tell us

**Will:**
- Whether the discipline skill changes pass-rate at all (signal vs noise).
- Whether scenario stratification (difficulty × category) shows where
  discipline matters most.
- Cost / wall-time / tool-call overhead of the disciplined bundle vs
  baseline — directly readable from arena's existing cost tracker.

**Won't (deferred to v1 / v2):**
- SWE-bench tasks — needs a differential gate (`FAIL_TO_PASS` /
  `PASS_TO_PASS`). See proposal §10.3.
- Multi-agent bundles (planner + executor, panel-of-experts) — needs
  A2A token-accounting verification + orchestrator decision. See
  proposal §10.5.

## Soft-metric capture

Each scenario runs `diff_stats.sh` (in the host-only `codegen-metrics`
skill) at conversation-end via `tool_exec`, capturing JSON
`{total_loc, impl_loc, test_loc, files_count}` into the per-result
`details.result` payload. Read with jq:

```bash
jq -r '.conversation_assertions.results[]
       | select(.details.tool == "Bash")
       | .details.result | fromjson
       | "loc=\(.total_loc) impl=\(.impl_loc) tests=\(.test_loc)"' \
   out/<run-id>.json
```

**Spike finding (single trial, both bundles 5/5 pass):** `total_loc`
and `impl_loc` are **identical** across baseline and disciplined for
every scenario at this difficulty — Sonnet at temp=0.1 converges on
the same edit shape regardless of skill. The differentiating signal
is **`run_lint` calls** (visible in `ToolStats.by_tool`):

| Scenario | Baseline tool calls | Disciplined |
|---|---|---|
| go-add-bugfix | Edit=1 Write=3 run_tests=2 | + run_lint=1 |
| go-binary-search | Edit=1 Write=3 run_tests=2 | + run_lint=1 |
| go-fizzbuzz-order | Edit=1 Write=3 run_tests=2 | + run_lint=1 |
| go-counter-race | Bash=2 Write=4 run_lint=1 run_tests=2 | (same) |
| go-strings-reverse | Edit=1 Write=3 run_tests=2 | (same) |

Disciplined runs lint on 3/5 scenarios that baseline skips — the
"verify before done" discipline manifests as explicit lint checks,
not different code shape. For harder tasks (where the agent has more
room to disagree) the LOC metric should start to differentiate; the
capture mechanism is in place for that.
