# RAG Agent Assertions Demo

Demonstrates the full named-primitive RAG suite — `faithfulness`, `answer_relevancy`, `contextual_precision`, `contextual_recall`, `contextual_relevancy`, `hallucination` — exercised as scenario assertions against a mock RAG assistant + mock LLM judge. Keyless and deterministic; the same scenarios work against real providers with one config swap.

## What it tests

One scenario, one question, six RAG-specific assertions on the answer:

- **`faithfulness`** — is the answer grounded in the retrieved context?
- **`answer_relevancy`** — does the answer address the user's question?
- **`contextual_precision`** — what fraction of retrieved chunks are relevant?
- **`contextual_recall`** — do retrieved chunks cover the ground-truth answer?
- **`contextual_relevancy`** — what's the mean per-chunk relevance?
- **`hallucination`** — is the answer free of unsupported claims (1.0 = no hallucination)?

Each is a thin wrapper over `llm_judge` with a hardened default prompt derived from public DeepEval / Ragas references (Apache 2.0). Default scoring is on `[0, 1]`; each assertion uses `min_score` for the pass threshold.

## Running

```bash
cd examples/rag-agent
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

Keyless: the agent is a mock provider; the judge is a mock provider too (returns a canned score of 0.92 for every call).

## Swapping in a real judge

For real RAG evaluation, replace the mock judge with an OpenAI / Anthropic / Gemini judge:

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

Register it in `config.arena.yaml` and update the `judges:` entry:

```yaml
judges:
  - name: rag-judge
    provider: openai-judge
```

Run with `OPENAI_API_KEY` in your environment. Each assertion now goes to the real LLM judge.

## Dynamic retrieval

The demo passes contexts inline in the scenario assertions. For a live RAG agent that decides what to retrieve at runtime, pass the chunks through metadata:

1. The retrieval tool writes its results to `evalCtx.Metadata` (e.g. `metadata["retrieved_chunks"]`).
2. The assertion uses `context_field`:

```yaml
- type: faithfulness
  params:
    context_field: retrieved_chunks
    judge: rag-judge
    min_score: 0.8
```

The RAG handlers all support three context forms in priority order: `contexts` (`[]string`), `context` (string), `context_field` (metadata key).

## File layout

```
rag-agent/
├── README.md
├── config.arena.yaml
├── mock-responses-assistant.yaml      # canned RAG assistant response
├── mock-responses-judge.yaml          # canned judge JSON (score 0.92)
├── prompts/rag-assistant.yaml
├── providers/
│   ├── mock-assistant.yaml
│   └── mock-judge.yaml
└── scenarios/paris-capital.scenario.yaml
```
