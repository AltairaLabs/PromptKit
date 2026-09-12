---
title: Memory and Grounding
sidebar:
  order: 6
---
Learn how to give an agent knowledge it did not get from the current conversation — either facts it manages itself, or content injected into its prompt before it runs.

## Two mechanisms, two jobs

PromptKit has two ways to put outside knowledge in front of a model. They are configured separately and answer different questions.

| | Memory tools | Ambient grounding |
|---|---|---|
| Who decides | The model, by calling a tool | The pipeline, every turn |
| Source | A `memory.Store` you provide | Any corpus, via a `memory.Retriever` you provide |
| Scoped to | A subject (a user, a workspace) | Whatever your retriever chooses |
| Reaches the model as | A tool result mid-turn | The `{{memory_context}}` template variable |
| Configured with | `WithMemory` | `WithRetriever` |

Use memory tools when the model should remember things about the person it is talking to. Use grounding when every answer should be anchored in your documentation, catalog, or knowledge base — the model never has to think to ask for it. [Retrieval Architecture](/runtime/explanation/retrieval-architecture/) covers the tradeoff in full: what each shape costs, and when to reach for which.

They compose. An agent can remember that a customer prefers email while grounding its answers in the current returns policy.

## Ambient grounding

Put the variable in your system prompt:

```json
{
  "prompts": {
    "support": {
      "system_template": "You are a support agent. Answer from the reference material below.\n\nReference material:\n{{memory_context}}"
    }
  }
}
```

Then wire a retriever:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/memory/corpus"
    "github.com/AltairaLabs/PromptKit/sdk"
)

kb := corpus.New([]corpus.Document{
    {ID: "returns", Title: "Returns policy", Text: "Faulty items may be returned within 37 days of delivery."},
    {ID: "shipping", Title: "Shipping", Text: "Orders ship the next business day."},
})

conv, _ := sdk.Open("./support.pack.json", "support",
    sdk.WithRetriever(kb),
)
```

Before each turn renders, the retriever is asked what is relevant to the conversation so far and its answer is substituted into `{{memory_context}}`. No store, no scope, no memory capability — grounding needs only a retriever.

Retrieval runs on every turn, so grounding follows the conversation. A turn that retrieves nothing renders the variable empty rather than inheriting the previous turn's material.

### Writing a retriever

`corpus.Retriever` is a reference implementation: it scores documents by term overlap with the latest user turn and returns the best few. It is for development and tests. Production hosts implement the interface against a real index:

```go
type Retriever interface {
    RetrieveContext(ctx context.Context, scope map[string]string, messages []types.Message) ([]*memory.Memory, error)
}
```

You get the turn's messages, so relevance can depend on the whole conversation rather than the last message alone, and the scope map, which is empty unless a memory capability supplied one. Use it to filter a shared index by tenant.

Return an empty slice when nothing is relevant. Injecting an unrelated document is worse than injecting none — the model cannot tell that it was not chosen.

### Formatting what gets injected

By default each item is rendered as `[type] content (confidence: N.N)`. Override that to surface whatever the prompt needs to cite:

```go
sdk.WithRetrievalFormatter(func(items []*memory.Memory) string {
    var b strings.Builder
    for _, m := range items {
        fmt.Fprintf(&b, "[%s] %s\n", m.ID, m.Content)
    }
    return b.String()
})
```

## Memory tools

`WithMemory` gives the model four tools — `memory__remember`, `memory__recall`, `memory__list` and `memory__forget` — backed by a store you supply:

```go
import "github.com/AltairaLabs/PromptKit/runtime/memory"

store := memory.NewInMemoryStore()
scope := map[string]string{"user_id": "u-1234"}

conv, _ := sdk.Open("./assistant.pack.json", "assistant",
    sdk.WithMemory(store, scope),
)
```

The model calls them on its own initiative, so the system prompt should say when to. There is no ambient injection here: nothing reaches the prompt unless the model asks.

### Scope

The scope map is the isolation boundary. Every store operation takes it, so `memory__recall` sees only what was saved under the same scope. Keys are yours to choose — `{"user_id": "u-1234", "workspace_id": "acme"}` is typical.

Scope also gates the tools. The capability looks for a subject key — `user_id` by default — and registers nothing when it is absent, so an anonymous conversation cannot write into a shared bucket:

```
WARN memory tools skipped: scope has no subject key  expected_key=user_id scope_keys=[]
```

Name a different key with `WithMemorySubjectKey("account_id")`.

`InMemoryStore` matches by substring and keeps nothing between processes; it exists for development and tests. Production stores implement `memory.Store` against a real backend, where a vector search replaces the substring match.

### Extraction

An extractor writes memories without the model calling a tool, from the conversation after each turn:

```go
sdk.WithMemory(store, scope,
    sdk.WithMemoryExtractor(myExtractor),
)
```

PromptKit defines the interface; platform layers implement it, typically with an LLM that classifies what is worth keeping.

### Retrieval without tools

To wire memory retrieval but keep the tools out of the model's hands, combine the capability's retriever with `WithMemoryToolsDisabled`:

```go
sdk.WithMemory(store, scope,
    sdk.WithMemoryRetriever(myRetriever),
    sdk.WithMemoryToolsDisabled(),
)
```

`WithMemoryContextFormatter` is the formatter for this path — the capability's equivalent of `WithRetrievalFormatter`. When both a capability retriever and `WithRetriever` are configured, `WithRetriever` wins.

### Backend-specific arguments

A store with capabilities the four tools do not model — graph expansion, point-in-time reads, a namespace — can accept extra arguments without forking PromptKit. Extend the tool's input schema with [`WithToolDescriptorOverride`](/sdk/reference/conversation-manager/#WithToolDescriptorOverride), and the executor forwards anything it does not type itself:

```go
conv, _ := sdk.Open("./assistant.pack.json", "assistant",
    sdk.WithMemory(store, scope),
    sdk.WithToolDescriptorOverride(memory.RecallToolName,
        func(d *tools.ToolDescriptor) {
            d.Description = "Recall memories, optionally expanding the graph from a seed."
            d.InputSchema = schemaWithSeedAndHops // adds seed_name, max_hops
        }),
)
```

Your store reads them off the options struct:

```go
func (s *GraphStore) Retrieve(
    ctx context.Context, scope map[string]string, query string, opts memory.RetrieveOptions,
) ([]*memory.Memory, error) {
    seed, _ := opts.Extras["seed_name"].(string)
    hops, _ := opts.Extras["max_hops"].(float64) // JSON numbers arrive as float64
    ...
}
```

`memory__list` works the same way through `ListOptions.Extras`, and `memory__remember` merges its extras into `Memory.Metadata` alongside the typed `metadata` argument, where a typed key wins a collision.

`memory__forget` is the one that needs opting in. `Store.Delete` takes no options parameter, so implement `memory.ExtrasDeleter` and the executor prefers it:

```go
func (s *GraphStore) DeleteWithOptions(
    ctx context.Context, scope map[string]string, memoryID string, opts memory.DeleteOptions,
) error
```

A store that ignores `Extras`, or does not implement `ExtrasDeleter`, behaves exactly as it did before — the extras are simply dropped. [Override Capability Tools](/sdk/how-to/tools/override-capability-tools/#getting-a-new-parameter-to-the-host) has the same matrix for workflow, A2A and skills.

Backend-specific fields in the **result** need nothing special: return them in each `Memory.Metadata` and they serialize into the tool result the model sees.

## Using both

```go
conv, _ := sdk.Open("./support.pack.json", "support",
    // Facts about this customer, managed by the model.
    sdk.WithMemory(store, map[string]string{"user_id": "u-1234"}),
    // Grounding from your content, injected every turn.
    sdk.WithRetriever(kb),
)
```

Keep the sources separate. Pointing grounding at the memory store means the model finds the same rows twice — once by asking, once without — and a filter you apply inside your `Retriever` does not run on the tool path, since the tool reaches the store directly. See [Retrieval Architecture](/runtime/explanation/retrieval-architecture/#keep-the-sources-apart).

## Duplex is not supported

Ambient grounding is a per-turn operation: the retrieval stage reads the turn's messages once the input closes, then writes the context the template renders. A duplex session has no such boundary — its input stays open until the session ends — so the stage would never forward and the provider session would never start.

`OpenDuplex` and `OpenVoice` therefore refuse a configured retriever rather than returning a session that cannot reply:

```
ambient grounding (a memory retriever) is not supported with a duplex provider: ...
```

Use the memory tools for retrieval in a voice or realtime session, or do the retrieval yourself and pass the result as a variable. Tracking in issue #1962.

## Gotchas

**The variable has to be in the prompt.** Retrieval runs and produces nothing visible if `{{memory_context}}` does not appear in `system_template`. Check the rendered prompt, not the retrieval log.

**An unresolved placeholder fails the turn.** If `{{memory_context}}` cannot be resolved — no retriever configured, for instance — `Send` returns an error rather than sending the model a prompt it cannot use:

```
system prompt has unresolved variables: support: unresolved template
placeholders: [{{memory_context}}] (variables available: customer_name)
```

**Your retriever sees the user's words verbatim.** Message text is never variable-substituted, so what reaches `RetrieveContext` is exactly what was sent — including any `{{...}}` a user happened to type.

## See also

- [Manage Context](/sdk/how-to/conversations/manage-context/) — token budget and truncation for long conversations
- [Memory reference](/runtime/reference/memory/) — `Store`, `Retriever`, `Extractor`, `Memory`
- [Memory Corpus reference](/runtime/reference/memory-corpus/) — the reference retriever
- [Retrieval Architecture](/runtime/explanation/retrieval-architecture/) — why retrieval has two shapes, and why PromptKit ships no retriever
