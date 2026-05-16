---
title: Test RAG agents with the standard primitive suite
description: Exercise faithfulness, answer_relevancy, contextual_precision/recall/relevancy, and hallucination as scenario assertions. Keyless CI via a mock judge; real LLM judge for production runs.
---

This how-to walks through `examples/rag-agent/` — the full named RAG primitive suite (`faithfulness`, `answer_relevancy`, `contextual_precision`, `contextual_recall`, `contextual_relevancy`, `hallucination`) exercised as scenario assertions. The vocabulary buyers from DeepEval / Ragas expect, wired in PromptArena.

## What it proves

RAG eval frameworks compete on the primitive catalog: faithfulness, answer relevancy, contextual recall, hallucination. PromptArena ships those primitives in `runtime/evals/handlers/` (added in #1145) as thin wrappers over `llm_judge` with hardened default prompts adapted from public DeepEval / Ragas references (Apache 2.0).

Each primitive is invokable as a scenario assertion with `min_score`. The demo runs all six against a single question + answer + retrieved context, with a mock LLM judge for keyless CI.

## The assertion shape

```yaml
turns:
  - role: user
    content: "What is the capital of France?"
    assertions:
      - type: faithfulness
        params:
          contexts:
            - "Paris is the capital and most populous city of France."
            - "Located on the Seine River in north-central France."
          judge: rag-judge
          min_score: 0.8

      - type: answer_relevancy
        params:
          judge: rag-judge
          min_score: 0.8

      - type: contextual_precision
        params:
          contexts: [...]
          judge: rag-judge
          min_score: 0.5

      # ... contextual_recall, contextual_relevancy, hallucination
```

All six assertions share the same shape — the eval handler does the work; the assertion config supplies thresholds and (for the chunk-based primitives) the context.

## Three context sources

Every RAG handler accepts retrieved context in three forms (preference order):

1. **`contexts: [...]`** — the canonical inline list (used in the demo).
2. **`context: "..."`** — single-chunk shorthand.
3. **`context_field: <metadata-key>`** — look up the chunks from `evalCtx.Metadata`. Use this when a retrieval tool writes results to metadata at runtime; the assertion reads them back.

For a live RAG agent, the dynamic `context_field` form is the right shape:

```yaml
- type: faithfulness
  params:
    context_field: retrieved_chunks
    judge: rag-judge
    min_score: 0.8
```

Wire your retrieval tool to set `metadata["retrieved_chunks"]` on each turn and the assertion auto-picks-up the chunks.

## Run it

```bash
cd examples/rag-agent
promptarena serve
```

Headless / CI:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

Keyless: both the RAG assistant and the LLM judge are mock providers. The mock judge returns `{"passed": true, "score": 0.92, "reasoning": "..."}` for every call; all six assertions pass with the default `min_score` thresholds.

## Swapping in a real judge

Replace the mock judge with a real LLM provider:

```yaml
# providers/openai-judge.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-judge
spec:
  id: openai-judge
  type: openai
  model: gpt-4o-mini
```

Update `judges:` in `config.arena.yaml`:

```yaml
judges:
  - name: rag-judge
    provider: openai-judge
```

Run with `OPENAI_API_KEY` set. The mock assistant can stay or get swapped too — the assertions don't care which provider produced the answer.

## CI gate

```yaml
# .github/workflows/rag-agent.yml
name: RAG agent

on:
  pull_request:
    paths:
      - 'examples/rag-agent/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run RAG scenarios
        working-directory: examples/rag-agent
        run: ../../bin/promptarena run --ci --formats json
```

The default config is keyless. Swap the mock judge for a real one when you want to grade real outputs.

## Naming and credit

The handler default prompts are adapted from public DeepEval and Ragas reference implementations (Apache 2.0). Attribution lives in each handler's docstring. The name choices (`faithfulness`, `answer_relevancy`, `contextual_*`, `hallucination`) match the buyer-facing vocabulary in the comparison-sheet bake-offs.

## Related how-tos

- The [Checks Reference](/reference/checks/) has the full parameter list and surface notes for each RAG primitive.
- The [Validate Outputs](/arena/how-to/validate-outputs/) how-to covers the broader assertion mechanism.
