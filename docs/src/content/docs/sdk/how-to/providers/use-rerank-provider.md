---
title: Rerank retrieved candidates
description: Configure a rerank provider with WithRerankProvider and call it from your own retrieval flow to reorder candidates by relevance.
sidebar:
  order: 30
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/providers/cohere/rerank.go
    - runtime/providers/cohere/rerank_register.go
    - runtime/providers/rerank.go
    - runtime/providers/rerank_factory.go
    - runtime/providers/voyageai/rerank.go
    - runtime/providers/voyageai/rerank_register.go
    - sdk/options.go
    - sdk/provider_file.go
    - sdk/rerank.go
    - sdk/runtime_config.go
---

Reorder a list of candidate passages by relevance to a query before you spend prompt budget on them.

## Prerequisites

- A retrieval step of your own, such as a vector search, that returns more candidates than the prompt can carry.
- An API key for a rerank provider in the environment: `VOYAGE_API_KEY` for `voyageai` or `COHERE_API_KEY` for `cohere`.

## Steps

### 1. Declare the provider

Pass `sdk.WithRerankProvider` when you open the conversation. A provider file with `role: rerank` declares the same thing.

```go
conv, err := sdk.Open("./assistant.pack.json", "assistant",
	sdk.WithRerankProvider(sdk.ProviderSpec{
		Type:  "voyageai",
		Model: "rerank-2.5",
	}),
)
if err != nil {
	return err
}
defer conv.Close()
```

The hosted `Type` values are `voyageai` and `cohere`. `mock` is an in-process provider for tests. The option fails to build with an error that names the registered types for any other value.

### 2. Call the provider

Nothing in the pipeline calls the reranker for you. Fetch the provider from the conversation and call it:

```go
rr, err := conv.RerankProvider()
if err != nil {
	return err
}
out, err := rr.Rerank(ctx, providers.RerankRequest{
	Query:     question,
	Documents: candidates,
	TopN:      5,
})
if err != nil {
	return err
}
for _, r := range out.Results {
	fmt.Println(r.Index, r.Score, candidates[r.Index])
}
```

`providers` is `github.com/AltairaLabs/PromptKit/runtime/v2/providers`.

`RerankRequest` fields:

| Field | Description |
|---|---|
| `Query` | The text to rank against. Required: an empty query is an error. |
| `Documents` | The candidates. An empty slice returns an empty result and no error. |
| `TopN` | The most results to return. Zero returns all of them, ordered. |
| `Model` | Overrides the provider's default model. |

Results come back best first. Key on `RankedDocument.Index`, the position in `Documents`, because the `Document` text is not always echoed. `Score` orders results within one provider and model, and is not comparable across providers.

`MaxDocuments()` on the provider reports the most candidates one call accepts. Batch your candidates when you can exceed it.

## Use more than one provider

Declare several `WithRerankProvider` options, for example a cheap reranker for a first pass and an accurate one for the survivors. Give each an `ID`:

| Method | Returns |
|---|---|
| `RerankProvider()` | The first provider you declared. |
| `RerankProviderByID(id)` | The provider with that ID, or an error listing the configured IDs. |
| `RerankProviderIDs()` | The IDs in declaration order. |

`RerankProvider()` returns an error when no provider is configured.

## Verify

Call `Rerank` with two or three short documents and a `TopN` of 1. The result holds the index of the most relevant document.

## Related

- [Configure embedding providers declaratively](/sdk/how-to/providers/declarative-embedding-providers/)
- [Memory and grounding](/sdk/how-to/conversations/use-memory/)
