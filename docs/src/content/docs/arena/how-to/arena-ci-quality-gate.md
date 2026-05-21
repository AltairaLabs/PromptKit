---
title: Run Arena as a CI quality gate
description: Wire promptarena run --ci into GitHub Actions as a hard gate. Fork-safe defaults, real-provider keys via secrets, threshold-based pass/fail, report uploads for reviewers.
---

This how-to is the recipe for gating merges on Arena scenarios. The general "integrate with CI/CD" how-to covers running tests; this one focuses on the **quality-gate** pattern — what to gate on, how to keep secrets safe in fork PRs, and how to surface failures for the reviewer.

## What "quality gate" means here

A quality gate is a CI check that exits non-zero on the kinds of regressions you'd want to catch before merge:

- **Behavioural drift** — same prompt, different model version produces different output (`examples/model-migration/`).
- **Tool-call regression** — agent stopped calling the right tool on the right path (`examples/voice-refund-demo/`, `examples/voice-ivr/`).
- **Safety regression** — a guardrail stopped firing on PII / toxicity / role-violation (`examples/voice-red-team/`).
- **Latency budget breach** — a refactor made the agent slower than the user-experience target (`examples/voice-latency-budget/`).

`promptarena run --ci` exits zero if all assertions pass, non-zero otherwise. Wire that into GitHub Actions as a required check and the bad merges stop landing.

## The fork-safe split pattern

Real-provider runs need provider keys, which means GitHub secrets, which fork PRs can't see. The standard pattern: split into two jobs.

```yaml
name: Arena quality gate

on:
  pull_request:
    branches: [main]

jobs:
  # Job 1: keyless. Runs on every PR including forks. Validates configs,
  # runs mock-provider scenarios. Cheap, fast, deterministic.
  validate-and-mock:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena

      - name: Validate all example configs
        run: |
          for cfg in examples/*/config.arena.yaml; do
            ./bin/promptarena validate "$cfg" || exit 1
          done

      - name: Run mock-provider scenarios
        run: |
          for dir in examples/voice-ivr examples/voice-red-team examples/text-negotiation examples/model-migration; do
            (cd "$dir" && ../../bin/promptarena run --ci --formats json) || exit 1
          done

  # Job 2: secret-gated. Skips for forks. Runs against real providers.
  real-providers:
    runs-on: ubuntu-latest
    if: github.event.pull_request.head.repo.full_name == github.repository
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena

      - name: Run voice-refund-demo against Gemini Live
        working-directory: examples/voice-refund-demo
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
          CARTESIA_API_KEY: ${{ secrets.CARTESIA_API_KEY }}
        run: ../../bin/promptarena run --ci --formats html,json --provider gemini-2-flash

      - name: Upload report
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: arena-report
          path: examples/voice-refund-demo/out/
```

The `if: github.event.pull_request.head.repo.full_name == github.repository` check fails the secret-bearing job for fork PRs (no `pull_request_target`, no secrets leak). Internal PRs run both jobs; external PRs only run the keyless one.

## Threshold-based pass/fail

The default assertion behaviour is binary: pass if every assertion in every scenario passes. For noisier real-provider runs, use `pass_threshold` per assertion or `trials` for stochastic checks:

```yaml
assertions:
  - type: assertion
    params:
      eval_type: llm_judge
      eval_params:
        criteria: "Agent stayed professional under pressure"
      min_score: 0.7
    pass_threshold: 0.8     # 80% of trials must pass

  - type: tools_called
    params:
      tool_names: [lookup_order]
    trials: 5                # run the same scenario 5 times
    pass_threshold: 0.8      # 4/5 must pass
```

Useful for flaky areas (LLM judges, prompts that depend on temperature) without dropping the gate entirely.

## What to upload for the reviewer

Failures are easier to diagnose when the reviewer can eyeball the HTML report. Standard step:

```yaml
- name: Upload report
  if: always()              # always run, even on test failure
  uses: actions/upload-artifact@v4
  with:
    name: arena-report
    path: examples/<example-name>/out/
    retention-days: 14
```

The HTML report contains per-scenario per-provider responses, per-assertion outcomes, and (for voice scenarios) inline audio playback. Reviewers can replay a failing turn without cloning the branch.

## Branch protection wiring

In your repo settings → Branches → Branch protection rule on `main`:

- Add `validate-and-mock` and `real-providers` to "Required status checks before merging."
- Keep `validate-and-mock` strictly required.
- Optionally make `real-providers` required as well; or leave it as an advisory check if cost is a concern.

For tight gates, also require:

- "Require linear history" so the report uploads correspond 1:1 to PR commits.
- "Require status checks to be up to date before merging" so the report reflects the most recent push.

## Threshold strategies per gate type

| Gate type | Recommended assertion shape | Why |
|---|---|---|
| Tool-call regression | `tools_called` / `tool_calls_with_args` / `tool_call_sequence` | Deterministic; binary pass/fail; cheap to run |
| Safety guardrail regression | `guardrail_triggered` | Reads `validations:` on the recorded message; no LLM cost; deterministic |
| Model migration | `content_includes` / `outcome_equivalent` / `max_length` | Compare per-model outputs side by side; CI fails if any cell regresses |
| Latency budget | `latency_budget` | Reads `LatencyMs` from the assistant message via the Arena bridge |
| LLM-judged quality | `type: assertion` wrapping `llm_judge` / `llm_judge_session`, with `min_score` on the wrapper + `pass_threshold` on the assertion | Use `pass_threshold` to tolerate stochastic noise; pair with `trials` for stability |
| RAG quality | `faithfulness` / `answer_relevancy` / `contextual_*` / `hallucination` | LLM-judged; same noise considerations |

## Failure recipes

- **Flaky LLM judge**: bump `trials` and `pass_threshold`; if still flaky, switch to a deterministic content check (`content_includes`, `content_excludes`) plus an LLM judge for "quality" rather than "correctness".
- **Provider rate limits**: run real-provider job on a schedule (nightly) instead of per-PR; keep the keyless job as the per-PR gate.
- **Cost concerns**: scope the real-provider job to scenarios under `paths:` filters that target only the directories you care about.
- **Cross-team review**: upload the report to a long-retention bucket (S3 / GCS); link from the PR description for stakeholders who don't have GitHub access.

## Related how-tos

- [Integrate with CI/CD](/arena/how-to/integrate-ci-cd/) — the general CI integration walkthrough across GitHub Actions, GitLab CI, Jenkins.
- [Run the same scenario across multiple providers](/arena/how-to/voice-bake-off/) — the cross-provider fan-out pattern that feeds the model-migration gate.
- [Gate model migrations on a regression suite](/arena/how-to/model-migration/) — concrete example using two providers + common assertion bar.
