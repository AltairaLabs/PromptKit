# Evals Handlers — Conventions

## The eval / assertion / guardrail split

Three roles share the same eval primitives. Keep them separated:

| Role | Where it lives | What it does |
|------|----------------|--------------|
| **Eval** | `runtime/evals/handlers/*.go` | Computes a signal. Emits `EvalResult.Score` = raw metric. No threshold logic. |
| **Assertion** | `type: assertion` wrapper (`runtime/evals/wrappers.go`) | Wraps an eval. Applies `min_score` / `max_score`. Decides pass/fail. |
| **Guardrail** | `type: guardrail` wrapper (`runtime/evals/wrappers.go`) | Wraps an eval. Decides whether to fire at runtime. |

The wrappers route to inner evals via the `EvalTypeRegistry`. One inner eval primitive can be reused by all three callers.

## Eval-primitive contract

Every handler in this package that takes a measurement (classify-backed, embedding-based, llm-judge, …) MUST:

1. **Emit `Score` as the raw signal.** For classifiers: the model's score for the chosen label. For embedders: the similarity. For LLM judges: the judge's numerical score. Score in `[0, 1]` by convention.
2. **Set `MetricValue` to the same value.** Reports use `MetricValue` for trend plots.
3. **Put structured detail in `Details`.** Anything the report or downstream consumer needs (the full label table, the embedding shape, the judge's reasoning) lives here, never in `Value`.
4. **Leave `Value` unset.** The assertion wrapper overwrites `Value` with a `bool` for pass/fail. Setting `Value` on the inner handler will be clobbered when wrapped — and confuses bare-eval usage in pack `evals:` blocks.
5. **REJECT `min_score` / `max_score`** at param-parse time. These are threshold params; they belong on the wrapper. Silently accepting them is a config-mistake trap. Use `parseClassifyConfig` (or the equivalent helper for your handler family) — it already enforces this.
6. **Skipped vs Error split.** Use `skippedResult` for infrastructure absence (no registry in context, no media of the right kind in the messages). Use `errorResult` for misconfigurations the user can fix (missing required param, out-of-range index). Skipped passes for free; Error fails the assertion.

## How to wire a new eval handler

1. Define the handler struct + `Type()` + `Eval()` in its own file.
2. Use the appropriate `parseXxxConfig` helper. For classify-backed handlers that's `parseClassifyConfig`.
3. Emit `Score`, `MetricValue`, `Details`. Never set `Value`. Never apply a threshold.
4. Register in `register.go` under the matching section.
5. Write the test pair: one happy path (Score is the expected value), one threshold-rejection test (`min_score: 0.5` returns an Error pointing at `type: assertion`).
6. Document in `docs/src/content/docs/reference/checks.md` under "Classify-backed Checks" (or the appropriate section) with the **two declaration sites** pattern: pack-level `evals:` example AND `type: assertion`-wrapped example.

## Anti-patterns — don't repeat these

- **Self-grading handlers.** Putting `min_score` / `max_score` logic inside the handler conflates the eval with the assertion. Audio_emotion / text_toxicity / text_sentiment were shipped that way originally; the cleanup landed in #1232. Don't reintroduce. The classify_handler_base.go param parser actively rejects threshold params on the eval — the test `TestAudioEmotion_RejectsThresholdParams` pins this behaviour.
- **Setting `Value` on the inner handler.** Wrapper overwrites it. Use `Details` instead.
- **Bundling "passed" boolean into the inner result.** Same problem — the wrapper decides pass/fail.
- **Two thresholds modes on the same handler** (`min_score` AND `max_score` selectable via flag). Pick neither — both are wrapper concerns.

## Legacy exceptions (none currently)

The LLM-judge family (`bias`, `toxicity`, `pii_leakage`, `role_violation`, `llm_judge`, `llm_judge_session`, the RAG primitives) used to accept `min_score` as a no-op param. That was cleaned up in #1233 — they now reject threshold params at parse time like every other classify-backed handler. There are no current exceptions to the convention.
