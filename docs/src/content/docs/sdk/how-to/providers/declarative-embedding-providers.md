---
title: Configure Embedding Providers Declaratively
description: Declare embedding providers in RuntimeConfig YAML and wire them into an SDK conversation
sidebar:
  order: 19
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - pkg/config/runtime_config.go
    - runtime/credentials/resolver.go
    - runtime/pipeline/stage/stages_context.go
    - runtime/providers/base_embedding.go
    - runtime/providers/bedrock/embedding.go
    - runtime/providers/bedrock/embedding_register.go
    - runtime/providers/embedding_factory.go
    - runtime/providers/embedding_platform.go
    - runtime/providers/gemini/embedding.go
    - runtime/providers/huggingface/embedding_register.go
    - runtime/providers/openai/embedding.go
    - runtime/providers/vertex/embedding.go
    - runtime/providers/vertex/embedding_register.go
    - runtime/providers/voyageai/embedding.go
    - runtime/providers/voyageai/embedding_register.go
    - sdk/options.go
    - sdk/provider_file.go
    - sdk/runtime_config.go
    - sdk/sdk.go
---

Declare an embedding provider in a `RuntimeConfig` file so selectors and context retrieval receive it without you constructing the provider in Go.

## Prerequisites

- A pack and a RuntimeConfig file loaded with `WithRuntimeConfig` (see [Use a RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/)).
- The provider's API key in an environment variable.

## Steps

### 1. Declare the provider

Add an entry to `spec.providers` with `role: embedding`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
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

The `type` is one of the values `sdk.RegisteredProviderTypes()["embedding"]` returns:

| `type` | Notes |
|---|---|
| `openai` | |
| `gemini` | |
| `voyageai` | |
| `ollama` | |
| `huggingface` | |
| `bedrock` | Requires a `platform` block |
| `vertex` | Requires a `platform` block |

### 2. Tune the provider (optional)

`additional_config` carries provider-specific keys:

| Key | Applies to | Effect |
|---|---|---|
| `dimensions` | every type | Vector size to request. The provider sends it to the API as `dimensions`, `outputDimensionality` or `output_dimension`, and fails any response whose vectors have a different length. |
| `input_type` | `voyageai` | `query` or `document`. |
| `embedding_format` | `bedrock` | `titan` or `cohere`: the request body the model takes. Any other value fails provider construction. |
| `batch_size` | `bedrock`, `vertex` | The most texts one request carries, reported by `MaxBatchSize`. Replaces the model family's default. |

Without `dimensions`, the provider reports the size of a model it knows. For any other model it reports `0` until the first response arrives.

A `bedrock` provider reads the request body from the model ID when `embedding_format` is not set: an `amazon.titan-embed` ID takes Titan's body and a `cohere.embed` ID takes Cohere's. Any other ID, such as an inference profile (`us.cohere.embed-english-v3`), an application inference profile ARN or a provisioned-throughput ARN, fails provider construction until `embedding_format` names the body. With `embedding_format` set, a Titan request carries a declared `dimensions` unless the ID names a Titan model other than `text-v2`, and Cohere requests never carry it.

A `vertex` provider builds for any model. A `text-embedding`, `textembedding-gecko` or `gemini-embedding` model gets that family's size and batch size; any other model gets a batch size of 1 and the size of its first response.

A `vertex` model can also be a full resource name. A publisher model (`projects/<project>/locations/<location>/publishers/google/models/<model>`) or an endpoint serving a tuned or self-deployed model (`projects/<project>/locations/<location>/endpoints/<id>`) is sent to `POST /v1/<name>:predict` on the platform's host. A publisher model's name gets its family's defaults. A model-registry name (`projects/<project>/locations/<location>/models/<id>`) has no `:predict` method and fails provider construction; deploy the model and use its endpoint name.

### 3. Open the conversation

An embedding provider declared in YAML becomes the conversation's default retrieval provider, so `Open` also requires a state store and a context window:

```go
conv, err := sdk.Open("./pack.json", "chat",
    sdk.WithRuntimeConfig("./runtime.yaml"),
    sdk.WithStateStore(statestore.NewMemoryStore()),
    sdk.WithContextWindow(20),
)
```

The first declared entry becomes the default retrieval provider and retrieves the top 3 matching older messages. A `state_store` block in the same file supplies the store, but a runtime config has no context window field, so pass `sdk.WithContextWindow(n)`. Selectors registered on the conversation receive the same instance as `SelectorContext.Embeddings` in `Init`, so a cosine-similarity selector and context retrieval share one embedding pool: one connection pool, one rate-limit bucket, one set of credentials.

`WithContextRetrieval(provider, topK)` overrides the declared default, including the count of 3, whichever order you apply the options in.

## Verify

`ValidateOptions` applies the options without opening a conversation, so it builds every declared provider:

```go
err := sdk.ValidateOptions(
    sdk.WithRuntimeConfig("./runtime.yaml"),
    sdk.WithStateStore(statestore.NewMemoryStore()),
    sdk.WithContextWindow(20),
)
```

A `nil` result means every provider builds. It resolves credentials but does not call the provider, so a wrong key still passes.

## Troubleshoot

| Error | Cause |
|---|---|
| `unsupported embedding provider type: <type>` | The `type` has no registered factory. `LoadRuntimeConfig` does not check it; the error appears when `Open` or `ValidateOptions` applies the option. |
| `an embedding provider is configured for retrieval ... which also requires WithContextWindow and WithStateStore (or state_store in the runtime config)` | An embedding provider is declared and the conversation lacks a context window, a state store, or both. The message names the missing options. |
| `duplicate provider id within role "embedding"` | Two embedding entries share an effective ID (the `id`, or the `type` when `id` is empty). `LoadRuntimeConfig` reports it, and also rejects an entry with no `type`. |
| `embedding provider "<id>": duplicate ID` | The same ID appears under `role: embedding` and in the top-level `embedding_providers:` block. |

The top-level `embedding_providers:` block takes the same entries without `role` and behaves the same. It is deprecated and removed in v3; move its entries to `spec.providers` with `role: embedding`.

## Related

- [Use a RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/)
- [Declarative TTS and STT providers](/sdk/how-to/providers/declarative-tts-stt-providers/): the same pattern with `role: tts` and `role: stt`.
