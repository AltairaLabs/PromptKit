---
title: Declarative Embedding Providers
description: Configure embedding providers for RAG and selectors from RuntimeConfig YAML
sidebar:
  order: 19
---

Embedding providers used to be Go-only: a consumer who wanted RAG retrieval or an embedding-backed selector had to import the provider package and pass an instance to `WithContextRetrieval`. As of #979, embedding providers can be declared in `RuntimeConfig` the same way chat providers are.

## Quick Start

```yaml
spec:
  embedding_providers:
    - id: rag
      type: openai
      model: text-embedding-3-small
      credential:
        credential_env: OPENAI_API_KEY
    - id: voyage
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

## Supported Types

| `type` value | Underlying package |
|---|---|
| `openai` | `runtime/providers/openai` |
| `gemini` | `runtime/providers/gemini` |
| `voyageai` | `runtime/providers/voyageai` |
| `ollama` | `runtime/providers/ollama` |

`additional_config` carries provider-specific extras. Currently honored:

- **VoyageAI** — `dimensions` (int), `input_type` (`query` | `document`)
- **Ollama** — `dimensions` (int)

## Programmatic Path Still Works

`WithContextRetrieval(provider, topK)` is unchanged. When set, it wins over the YAML default. This mirrors how chat providers behave: programmatic options take precedence over RuntimeConfig defaults.

## Validation

`LoadRuntimeConfig` rejects:

- Missing `type`.
- A `type` outside the supported set.
- Two entries with the same effective ID (explicit ID, or `type` when ID is omitted).

## Related

- [Use a RuntimeConfig](/sdk/how-to/use-runtime-config/)
- [Plug in an external selector](/sdk/how-to/) — selectors receive embedding providers via `SelectorContext.Embeddings`.

## Roadmap

TTS and STT providers follow the same pattern (#979). They're declared at the same level once landed; today they're still programmatic-only via `WithTTS` / `WithVADMode`.
