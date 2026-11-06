---
title: Memory and Grounding
description: Give a conversation knowledge from outside the current chat, by injecting retrieved content into the prompt or by giving the model memory tools.
sidebar:
  order: 6
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/memory/corpus/corpus.go
    - runtime/memory/formatter.go
    - runtime/memory/memory_store.go
    - runtime/memory/retriever.go
    - runtime/memory/store.go
    - runtime/memory/tools.go
    - runtime/memory/types.go
    - runtime/pipeline/stage/errors.go
    - runtime/pipeline/stage/stages_memory.go
    - runtime/pipeline/stage/stages_utilities.go
    - runtime/template/renderer.go
    - sdk/capability_memory.go
    - sdk/conversation.go
    - sdk/internal/pipeline/builder.go
    - sdk/options.go
    - sdk/voice.go
---
Give a conversation knowledge it did not get from the current chat, either by injecting retrieved content into the system prompt (ambient grounding) or by giving the model tools to manage facts about its subject (memory tools).

[Retrieval Architecture](/runtime/explanation/retrieval-architecture/) explains what each shape costs and when to use which.

## Prerequisites

- A conversation opened with `sdk.Open()`.
- For grounding, a `memory.Retriever`. For memory tools, a `memory.Store`.

## Ground answers in your content

1. Put `{{memory_context}}` in the prompt's system template:

   ```json
   {
     "prompts": {
       "support": {
         "system_template": "You are a support agent. Answer from the reference material below.\n\nReference material:\n{{memory_context}}"
       }
     }
   }
   ```

2. Pass a retriever to `Open()`:

   ```go
   import (
       "github.com/AltairaLabs/PromptKit/runtime/v2/memory/corpus"
       "github.com/AltairaLabs/PromptKit/sdk/v2"
   )

   kb := corpus.New([]corpus.Document{
       {ID: "returns", Title: "Returns policy", Text: "Faulty items may be returned within 37 days of delivery."},
       {ID: "shipping", Title: "Shipping", Text: "Orders ship the next business day."},
   })

   conv, err := sdk.Open("./support.pack.json", "support",
       sdk.WithRetriever(kb),
   )
   ```

Before each turn renders, the retriever receives the conversation so far, and the result replaces `{{memory_context}}`. Grounding needs no store, no scope and no memory capability. A turn that retrieves nothing renders the variable empty.

### Write a retriever

`corpus.Retriever` scores documents by term overlap with the latest user turn and returns the top three. Use it for development and tests. In production, implement `memory.Retriever` against your index:

```go
type Retriever interface {
    RetrieveContext(ctx context.Context, scope map[string]string, messages []types.Message) ([]*memory.Memory, error)
}
```

You receive the turn's messages, and a scope map that is empty unless a memory capability supplied one. Return an empty slice when nothing is relevant.

### Format the injected text

The default format is one line per item: `[type] content (confidence: N.N)`. Override it with `WithRetrievalFormatter`:

```go
sdk.WithRetrievalFormatter(func(items []*memory.Memory) string {
    var b strings.Builder
    for _, m := range items {
        fmt.Fprintf(&b, "[%s] %s\n", m.ID, m.Content)
    }
    return b.String()
})
```

## Give the model memory tools

1. Create a store and a scope, and pass them to `WithMemory`:

   ```go
   import "github.com/AltairaLabs/PromptKit/runtime/v2/memory"

   store := memory.NewInMemoryStore()
   scope := map[string]string{"user_id": "u-1234"}

   conv, err := sdk.Open("./assistant.pack.json", "assistant",
       sdk.WithMemory(store, scope),
   )
   ```

2. Run the conversation. The model sees four tools (`memory__remember`, `memory__recall`, `memory__list` and `memory__forget`) and calls them on its own initiative. Each tool's description says when to use it, so the system prompt does not need to.

`InMemoryStore` matches by case-insensitive substring and keeps nothing between processes. In production, implement `memory.Store` against your backend.

To change what the model sees for a tool, patch its descriptor with [`WithToolDescriptorOverride`](/sdk/how-to/tools/override-capability-tools/). Keep the system prompt for policy a description cannot carry, such as what not to store.

### Scope

The scope map is the isolation boundary: every store operation takes it, so `memory__recall` sees only what was saved under the same scope. The keys are yours to choose.

The capability registers no tools unless the scope has a non-empty subject key (`user_id` by default). Without one, it logs a warning and skips the tools:

```text
WARN memory tools skipped: scope has no subject key  expected_key=user_id scope_keys=[]
```

Name a different key with `sdk.WithMemorySubjectKey("account_id")`. Passing an empty string removes the check.

### Extract memories automatically

An extractor writes memories from the conversation after each turn, without the model calling a tool:

```go
sdk.WithMemory(store, scope,
    sdk.WithMemoryExtractor(myExtractor),
)
```

`myExtractor` implements `memory.Extractor`.

### Retrieval without tools

To inject memories into the prompt but keep the tools away from the model, combine `WithMemoryRetriever` with `WithMemoryToolsDisabled`:

```go
sdk.WithMemory(store, scope,
    sdk.WithMemoryRetriever(myRetriever),
    sdk.WithMemoryToolsDisabled(),
)
```

`WithMemoryRetriever` alone injects `{{memory_context}}` every turn and leaves the tools registered. `WithMemoryToolsDisabled` without a retriever disables memory entirely. `WithMemoryContextFormatter` is the formatter for this path. When both `WithRetriever` and a capability retriever are set, `WithRetriever` wins.

### Backend-specific arguments

If your store supports options the four tools do not model (graph expansion, point-in-time reads, a namespace), extend a tool's input schema with [`WithToolDescriptorOverride`](/sdk/reference/conversation-manager/#WithToolDescriptorOverride). The executor forwards the arguments it does not type itself:

```go
conv, err := sdk.Open("./assistant.pack.json", "assistant",
    sdk.WithMemory(store, scope),
    sdk.WithToolDescriptorOverride(memory.RecallToolName,
        func(d *tools.ToolDescriptor) {
            d.Description = "Recall memories, optionally expanding the graph from a seed."
            d.InputSchema = schemaWithSeedAndHops // adds seed_name, max_hops
        }),
)
```

Your store reads them from `RetrieveOptions.Extras`:

```go
func (s *GraphStore) Retrieve(
    ctx context.Context, scope map[string]string, query string, opts memory.RetrieveOptions,
) ([]*memory.Memory, error) {
    seed, _ := opts.Extras["seed_name"].(string)
    hops, _ := opts.Extras["max_hops"].(float64) // JSON numbers arrive as float64
    // ...
}
```

`memory__list` forwards extras through `ListOptions.Extras`. `memory__remember` merges them into `Memory.Metadata`, and a typed `metadata` key wins a collision.

`Store.Delete` takes no options, so `memory__forget` forwards extras only to stores that implement `memory.ExtrasDeleter`:

```go
func (s *GraphStore) DeleteWithOptions(
    ctx context.Context, scope map[string]string, memoryID string, opts memory.DeleteOptions,
) error
```

A store that does not read `Extras` drops them. Fields you return in `Memory.Metadata` serialize into the tool result without any extra setup. [Override Capability Tools](/sdk/how-to/tools/override-capability-tools/#4-read-the-new-parameter-in-your-host) has the matrix for workflow, A2A and skills.

## Use both

```go
conv, err := sdk.Open("./support.pack.json", "support",
    sdk.WithMemory(store, map[string]string{"user_id": "u-1234"}),
    sdk.WithRetriever(kb),
)
```

Keep the sources separate. Pointing grounding at the memory store surfaces the same rows twice, and a filter inside your `Retriever` does not run on the tool path. See [Retrieval Architecture](/runtime/explanation/retrieval-architecture/#keep-the-sources-apart).

## Verify

- Grounding: log the rendered system prompt, or have the model quote the reference material.
- Tools: send "Remember that I prefer email", then call `store.List(ctx, scope, memory.ListOptions{})`. The saved memory appears in the result.

## Troubleshoot

`OpenDuplex` and `OpenVoice` return an error when a retriever is configured, because the retrieval stage waits for the turn's input to close and a duplex session's input stays open:

```text
ambient grounding (a memory retriever) is not supported with a duplex provider: ...
```

In a voice or realtime session, use the memory tools, or retrieve yourself and pass the result as a variable.

The model never sees retrieved text if `{{memory_context}}` is missing from `system_template`. Check the rendered prompt, not the retrieval log.

A turn fails when `{{memory_context}}` cannot be resolved, for instance with no retriever configured:

```text
stage 'template' (transform) failed: system prompt has unresolved variables:
support: unresolved template placeholders: [{{memory_context}}] (variables available: customer_name)
```

Your retriever receives user text verbatim, including any `{{...}}` the user typed.

## Related

- [Manage Context](/sdk/how-to/conversations/manage-context/): token budget and truncation for long conversations
- [Memory reference](/runtime/reference/memory/): `Store`, `Retriever`, `Extractor`, `Memory`
- [Memory Corpus reference](/runtime/reference/memory-corpus/): the reference retriever
- [Retrieval Architecture](/runtime/explanation/retrieval-architecture/): why retrieval has two shapes
