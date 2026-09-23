---
title: Configure Embedding Providers Declaratively
description: Configure embedding providers for RAG and selectors from RuntimeConfig YAML
sidebar:
  order: 19
---

Embedding providers used to be Go-only: a consumer who wanted RAG retrieval or an embedding-backed selector had to import the provider package and pass an instance to `WithContextRetrieval`. As of #979, embedding providers can be declared in `RuntimeConfig` the same way chat providers are.

## Quick Start

Declare the provider under `spec.providers` with `role: embedding`:

```yaml
spec:
  providers:
    - id: rag
      role: embedding
      type: openai
      model: text-embedding-3-small
      credential:
        credential_env: OPENAI_API_KEY
    - id: voyage
      role: embedding
      type: voyageai
      model: voyage-3
      credential:
        credential_env: VOYAGE_API_KEY
      additional_config:
        dimensions: 1024
        input_type: query
```

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

The first declared entry becomes the default RAG provider unless `WithContextRetrieval` set one programmatically. The same instance is supplied to in-process `selection.Selector` implementations via `SelectorContext.Embeddings` on `Init`, so a cosine-similarity selector and the RAG retrieval pipeline share a single embedding pool — one connection pool, one rate-limit bucket, one set of credentials.

## The `embedding_providers:` block

Before role routing, embedding providers had their own top-level block:

```yaml
spec:
  embedding_providers:
    - id: rag
      type: openai
      model: text-embedding-3-small
      credential:
        credential_env: OPENAI_API_KEY
```

It still works and behaves identically. It is deprecated in favor of
`role: embedding` and is removed in v3. Declaring the same ID in both
spellings is rejected rather than silently resolved.

## Supported Types

| `type` value | Underlying package |
|---|---|
| `openai` | `runtime/providers/openai` |
| `gemini` | `runtime/providers/gemini` |
| `voyageai` | `runtime/providers/voyageai` |
| `ollama` | `runtime/providers/ollama` |

`additional_config` carries provider-specific extras. Currently honored:

- **All types** — `dimensions` (int): the vector size you want. It is sent to
  the API (`dimensions`, `outputDimensionality` or `output_dimension`), which
  shortens the vector where the model supports it. Every response is checked
  against it, so a model that cannot produce that size fails loudly instead of
  returning vectors your storage was not sized for.
- **VoyageAI** — `input_type` (`query` | `document`)

Without `dimensions`, the provider reports the size of a model it knows, and
for any other model — typically one behind an OpenAI-compatible server —
reports `0` until the first response gives the real size. It never guesses.

## Programmatic Path Still Works

`WithContextRetrieval(provider, topK)` is unchanged. When set, it wins over the YAML default. This mirrors how chat providers behave: programmatic options take precedence over RuntimeConfig defaults.

## Validation

`LoadRuntimeConfig` rejects:

- Missing `type`.
- A `type` outside the supported set.
- Two entries with the same effective ID (explicit ID, or `type` when ID is omitted).

## Related

- [Use a RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/)
- [Plug in an external selector](/sdk/how-to/) — selectors receive embedding providers via `SelectorContext.Embeddings`.

## See also

TTS and STT providers follow the same pattern, declared at the same level with `role: tts` / `role: stt` — see [declarative TTS and STT providers](/sdk/how-to/providers/declarative-tts-stt-providers/).
