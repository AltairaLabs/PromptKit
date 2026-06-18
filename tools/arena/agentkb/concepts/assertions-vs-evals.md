---
id: assertions-vs-evals
title: Assertions judge; evals measure
summary: Eval handlers emit a raw score; assertions apply a threshold. Never put the threshold on the eval.
tags: [assertions, evals, scoring]
---
PromptArena is an **assertion** framework. Eval handlers are *inputs* to assertions:
an eval handler emits `Score` as a raw signal (0..1) and nothing else. The pass/fail
threshold lives on a `type: assertion` wrapper:

```yaml
assertions:
  - type: assertion
    eval:
      type: toxicity        # eval handler — emits a raw score
    max_score: 0.2          # threshold lives HERE, on the assertion
```

Putting `min_score`/`max_score` on the inner eval is a common trap — the eval must
stay a pure primitive. Guardrails reuse the same eval primitives but enforce in
production; assertions are test-only and may observe guardrail firings.
