---
title: Manage Context
description: Keep long conversations inside the model's context window with a token budget, relevance truncation, a hot window, retrieval, summaries and tool-result compaction.
sidebar:
  order: 5
verified:
  commit: dc6d6f37cfabc0079dd9af47c52502e83f2a85af
  sources:
    - runtime/events/types.go
    - runtime/pipeline/stage/compactor.go
    - runtime/pipeline/stage/compactor_fold.go
    - runtime/pipeline/stage/relevance_embedding_cache.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/pipeline/stage/stages_utilities.go
    - runtime/providers/claude/context_window_table.go
    - runtime/providers/gemini/embedding.go
    - runtime/providers/ollama/context_window.go
    - runtime/providers/openai/embedding.go
    - runtime/providers/voyageai/embedding.go
    - runtime/statestore/interface.go
    - runtime/statestore/memory.go
    - runtime/statestore/redis.go
    - sdk/conversation.go
    - sdk/internal/pipeline/builder.go
    - sdk/options.go
    - sdk/sdk.go
---
Keep a long conversation inside the model's context window.

## Prerequisites

- A pack and a provider key. See [Open a Conversation](/sdk/how-to/conversations/initialize/).
- For relevance truncation or retrieval: an embedding provider (see [Create an embedding provider](#create-an-embedding-provider)).
- For the hot window, retrieval and summaries: a state store, for example `statestore.NewMemoryStore()` or `statestore.NewRedisStore(client)`.

## Truncate to a token budget

1. Set a budget. When the history exceeds it, the SDK drops the oldest messages first (the `"sliding"` strategy, the default). With no budget, nothing is truncated:

   ```go
   conv, err := sdk.Open("./app.pack.json", "chat",
       sdk.WithTokenBudget(8000),
   )
   ```

   `sdk.WithTruncation` selects another strategy and takes effect only with a budget:

   | Strategy | Behavior when over budget |
   |---|---|
   | `"sliding"` (default), `"oldest"` | Drops the oldest messages first |
   | `"summarize"` | Replaces old messages with a summary from the provider given to `WithAutoSummarize`; without one, drops the oldest |
   | `"relevance"` | Drops the least relevant messages (set by `WithRelevanceTruncation`) |
   | `"fail"` | Returns an error |

   An unrecognized value behaves as `"sliding"`.

   Every strategy that truncates drops whole turns. The kept messages start at a user message, and a tool call is kept only together with all of its results. A reply or tool exchange left at the cut is dropped as well, even if it would fit in the budget. `"summarize"` adds it to the summary. If the budget cannot fit any user message, the send fails with a `token budget exceeded` error instead of sending a request with no question in it.

2. To keep the messages most similar to the latest user message instead, use relevance truncation:

   ```go
   conv, err := sdk.Open("./app.pack.json", "chat",
       sdk.WithTokenBudget(8000),
       sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
           EmbeddingProvider:    embProvider,
           MinRecentMessages:    3,
           SimilarityThreshold:  0.3,
           AlwaysKeepSystemRole: true,
       }),
   )
   ```

   `MinRecentMessages` (default 3) always keeps the newest messages. `SimilarityThreshold` (default 0, which disables it) drops unprotected messages scoring below it. `AlwaysKeepSystemRole` (default `false`) keeps system-role messages whatever their score.

3. Change what messages are compared against with `QuerySource`:

   | `QuerySource` | Compared against |
   |---|---|
   | `"last_user"` (default) | The most recent user message |
   | `"last_n"` | The last `LastNCount` messages (default 3) |
   | `"custom"` | The text in `CustomQuery` |

   If the embedding call fails, the SDK falls back to dropping the oldest messages.

4. To stop re-embedding the whole history on every truncation, set `CacheEmbeddings: true`. The conversation then embeds only the messages (and query) it has no vector for yet:

   ```go
   sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
       EmbeddingProvider: embProvider,
       CacheEmbeddings:   true,
   }),
   ```

   A vector is keyed on the exact text embedded plus the embedding provider's ID and model, not on the message's position. An edited message is embedded again; a reordered one is not, and switching provider or model embeds everything again. The cache lives as long as the conversation (a `Fork` starts empty) and after each truncation keeps only the vectors that truncation used, so it never outgrows one turn's history. It is off by default.

## Load a hot window of recent messages

Use these options with a state store instead of loading the full history on every turn. `WithContextWindow` requires `WithStateStore`; set `WithConversationID` to resume the same history later.

1. Load only the last N messages. When the store implements `statestore.MessageReader` and `statestore.MessageAppender` (as `RedisStore` and `MemoryStore` do), the SDK loads the tail and appends new messages. Otherwise it loads and saves the whole history:

   ```go
   conv, err := sdk.Open("./app.pack.json", "chat",
       sdk.WithStateStore(store),
       sdk.WithConversationID("session-123"),
       sdk.WithContextWindow(20),
   )
   ```

2. Add semantic retrieval. Each turn, the SDK embeds the user's message and inserts the best matches from older messages, in chronological order between the summaries and the hot window. `WithContextRetrieval` requires `WithContextWindow` and `WithStateStore`. An embedding provider declared in a runtime config, or added with `WithEmbeddingProvider`, enables the same retrieval with a top-k of 3:

   ```go
   sdk.WithContextRetrieval(embProvider, 5), // top 5 matches
   ```

3. Add summaries. When the message count passes the threshold, the SDK summarizes the oldest unsummarized batch with `summaryProvider` and prepends the summaries as system messages. `WithAutoSummarize` requires `WithContextWindow`. A cheaper model works for the summary provider:

   ```go
   sdk.WithAutoSummarize(summaryProvider, 50, 10), // threshold 50 messages, batches of 10
   ```

All three together:

```go
func openLongConversation(
    store statestore.Store,
    embProvider providers.EmbeddingProvider,
    summaryProvider providers.Provider,
) (*sdk.Conversation, error) {
    return sdk.Open("./app.pack.json", "chat",
        sdk.WithStateStore(store),
        sdk.WithConversationID("session-123"),
        sdk.WithContextWindow(20),
        sdk.WithContextRetrieval(embProvider, 5),
        sdk.WithAutoSummarize(summaryProvider, 50, 10),
    )
}
```

`statestore` and `providers` are packages under `github.com/AltairaLabs/PromptKit/runtime/v2`.

## Control tool-result compaction

During multi-round tool execution, the context compactor runs between rounds and folds stale tool results into summaries such as `[file_read: package main... — 12000 bytes compacted]`. It is on by default:

- It triggers when the estimated context exceeds 70% of the provider's context window. The window comes from the provider when it implements `providers.ContextWindowProvider`, otherwise 128,000 tokens. The Claude, OpenAI and Gemini providers report a limit for each model in their pricing tables, and `additional_config.context_window` overrides it. Ollama and vLLM report a limit only through that setting. A model the table does not list reports 0, so the compactor keeps the 128,000 default.
- It folds the oldest tool results first and keeps the 4 most recent messages as written.
- It leaves error results, system messages and non-tool messages unchanged.
- It emits a `context.compacted` event with the token counts.

To turn it off:

```go
conv, err := sdk.Open("./app.pack.json", "chat",
    sdk.WithCompaction(false),
)
```

## Create an embedding provider

Each constructor reads its API key from the environment.

| Package | Constructor | Default model | Key variable |
|---|---|---|---|
| `providers/openai` | `openai.NewEmbeddingProvider()` | `text-embedding-3-small` | `OPENAI_API_KEY` |
| `providers/gemini` | `gemini.NewEmbeddingProvider()` | `gemini-embedding-001` | `GEMINI_API_KEY` or `GOOGLE_API_KEY` |
| `providers/voyageai` | `voyageai.NewEmbeddingProvider()` | `voyage-3.5` | `VOYAGE_API_KEY` |

Choose another model with `openai.WithEmbeddingModel`, `gemini.WithGeminiEmbeddingModel` or `voyageai.WithModel`:

```go
embProvider, err := openai.NewEmbeddingProvider(
    openai.WithEmbeddingModel(openai.EmbeddingModel3Large),
)
```

`voyageai.WithInputType(voyageai.InputTypeQuery)` sets the Voyage input type hint.

## Verify

Run a conversation with a budget and relevance truncation:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    ctx := context.Background()

    embProvider, err := openai.NewEmbeddingProvider()
    if err != nil {
        log.Fatal(err)
    }

    conv, err := sdk.Open("./app.pack.json", "chat",
        sdk.WithTokenBudget(8000),
        sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
            EmbeddingProvider:   embProvider,
            MinRecentMessages:   3,
            SimilarityThreshold: 0.3,
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    for _, msg := range []string{
        "I'm having trouble with my account billing",
        "What's the weather like today?",
        "Back to my billing: can you help fix it?",
    } {
        resp, err := conv.Send(ctx, msg)
        if err != nil {
            log.Fatal(err)
        }
        fmt.Printf("User: %s\nAssistant: %s\n\n", msg, resp.Text())
    }
}
```

The program prints a reply for each message.

## Related

- [Open a Conversation](/sdk/how-to/conversations/initialize/)
- [Manage Variables](/sdk/how-to/conversations/manage-state/)
- [Long Conversation Example](/sdk/examples/long-conversation/)
- [Arena Context Management](https://promptarena.altairalabs.ai/arena/how-to/manage-context/): the YAML configuration
