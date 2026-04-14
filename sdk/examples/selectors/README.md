# Selector Examples

Reference implementations of the `selection.Selector` interface
(see issue #980). PromptKit core ships only the exec client; everything
in here is example code consumers can copy, adapt, or import directly.

Two patterns covered:

| Path | Pattern | Lives | Wire-up |
|---|---|---|---|
| [`cosine/`](./cosine) | In-process Go: cosine similarity over PromptKit `EmbeddingProvider` vectors | inside the SDK process | `sdk.WithSelector(name, impl)` |
| [`exec_rerank/`](./exec_rerank) | External subprocess: forwards to a hosted rerank API | separate process (host or sandbox) | `spec.selectors.<name>.command` in RuntimeConfig |

The two paths are functionally interchangeable from PromptKit's point
of view — pick based on operational preference (deployment unit, language,
hot-path latency, blast-radius isolation).

---

## In-Process: Cosine Selector

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/sdk/examples/selectors/cosine"
)

emb, _ := openai.NewEmbeddingProvider()
sel := cosine.New("skills_local", emb, cosine.Options{TopK: 5})

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithSelector("skills_local", sel),
    sdk.WithRuntimeConfig("./runtime.yaml"), // spec.skills.selector: skills_local
)
```

The selector caches candidate embeddings keyed on `(ID, Description)`,
so a stable skill catalog only embeds once across many `Send` calls.
The query embedding is recomputed each turn (it changes every turn).

If `WithContextRetrieval` already configured an embedding provider for
RAG, that instance is supplied to `Init` via `SelectorContext.Embeddings`
and overrides the constructor argument — one provider, one connection
pool, one rate-limit bucket.

---

## External Process: Rerank Script

```yaml
spec:
  selectors:
    rerank:
      command: python
      args: [/selectors/rerank.py]
      env: [RERANK_API_KEY, RERANK_URL]
      timeout_ms: 3000
      # sandbox: sidecar     # optional — runs the script inside a k8s sidecar
  skills:
    selector: rerank          # narrow skill__activate's index per turn
  tool_selector: rerank       # narrow the LLM-visible pack tools per turn
```

(`tool_selector` is a flat field rather than nested under `tools:`
because the existing `spec.tools` map binds exec tool implementations.)

The wire protocol is:

```jsonc
// stdin
{
  "query":      {"text": "...", "kind": "skill", "pack_id": "...", "k": 5},
  "candidates": [{"id": "...", "name": "...", "description": "...", "metadata": {}}]
}

// stdout
{"selected": ["id1", "id2"], "reason": "optional"}
```

The bundled `rerank.py` calls a remote rerank endpoint when
`RERANK_URL` and `RERANK_API_KEY` are set; otherwise it falls back to a
trivial token-overlap ranker so the example runs without external
dependencies.

Combine with the sandbox examples in [`../sandboxes/`](../sandboxes/) to
run the selector inside a docker container or kubectl-exec sidecar
without changing the script.

---

## Behavior Notes

- A selector returning an error or an empty result is non-fatal —
  PromptKit falls back to "include all eligible" so a misconfigured
  ranker can never break a conversation.
- The `Query.Kind` field carries `"skill"` (set from `spec.skills.selector`)
  or `"tool"` (set from `spec.tool_selector`). A single selector
  implementation can dispatch on `kind` to serve both hook points;
  one binding under `spec.selectors.<name>` is fine. Tools narrowing
  preserves system tools (`skill__`, `a2a__`, `workflow__`, `mcp__`,
  `memory__`) regardless of selection — those are always available
  to the LLM.
- Selectors are called once per `Send` (per turn). Internal caching is
  the implementation's responsibility; the cosine example is one
  reasonable shape for it.
