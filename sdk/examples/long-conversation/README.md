# Long Conversation Context Management

## Overview

As conversations grow, they create two problems:

1. **Context overflow** — the message history exceeds the LLM's context window
2. **I/O cost** — loading and saving the full history on every turn is expensive

The PromptKit SDK solves both with a three-tier context system:

```
┌─────────────────────────────────────────────┐
│              Context Assembly                │
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  │
│  │ Summaries│→ │ Retrieved│→ │Hot Window  │  │
│  │ (oldest) │  │ (relevant│  │ (recent N) │  │
│  │          │  │  older)  │  │            │  │
│  └──────────┘  └──────────┘  └───────────┘  │
│                                              │
│  Sent to LLM: summaries + retrieved + recent │
└─────────────────────────────────────────────┘
```

- **Hot window**: Only the last N messages are loaded from the store each turn
- **Semantic retrieval**: Older messages relevant to the current query are found via embeddings
- **Auto-summarization**: When message count exceeds a threshold, old turns are compressed into summaries

## Features Demonstrated

- `WithContextWindow(n)` — load only the N most recent messages
- `WithContextRetrieval(embProvider, topK)` — semantic search for relevant older messages
- `WithAutoSummarize(provider, threshold, batchSize)` — compress old turns into summaries
- `WithStateStore(store)` — persistent conversation storage
- `WithConversationID(id)` — explicit session identity for state persistence

## Prerequisites

- Go 1.21+
- OpenAI API key (for the LLM, embeddings, and summarization)

## Running the Example

```bash
export OPENAI_API_KEY=sk-...
go run .
```

## How It Works

### Context Window

```go
sdk.WithContextWindow(4)
```

Instead of loading the entire conversation from the state store, only the last 4 messages are loaded. This uses the `ContextAssemblyStage` pipeline stage internally. When the store implements `MessageReader` (as both `MemoryStore` and `RedisStore` do), only the tail is deserialized — no full-state load required.

New messages are saved incrementally via `IncrementalSaveStage`, which uses `MessageAppender` when available.

### Semantic Retrieval

```go
embProvider, _ := openai.NewEmbeddingProvider()
sdk.WithContextRetrieval(embProvider, 3)
```

On each turn, the user's message is embedded and compared against older messages (those outside the hot window). The top 3 most semantically similar messages are retrieved and inserted chronologically between summaries and the hot window.

This ensures that when a user says "going back to the billing issue," the relevant earlier billing messages are included even though they've scrolled out of the hot window.

### Auto-Summarization

```go
summaryProvider := openai.NewProvider(
    "summarizer", "gpt-4o-mini", "https://api.openai.com/v1",
    providers.ProviderDefaults{MaxTokens: 1024}, false,
)
sdk.WithAutoSummarize(summaryProvider, 6, 4)
```

When the total message count exceeds 6, the 4 oldest unsummarized messages are compressed into a summary using the specified LLM provider. Summaries are prepended to the context as system messages, preserving key information while reducing token count.

Using a smaller model like `gpt-4o-mini` for summarization keeps costs low while the primary conversation can use a more capable model.

## Configuration Reference

| Option | Parameters | Default | Description |
|--------|-----------|---------|-------------|
| `WithContextWindow` | `recentMessages int` | — | Number of recent messages to load per turn |
| `WithContextRetrieval` | `embProvider, topK int` | — | Embedding provider and number of messages to retrieve |
| `WithAutoSummarize` | `provider, threshold, batchSize int` | — | LLM provider, message count trigger, and batch size |
| `WithStateStore` | `store statestore.Store` | — | State store for persistence (required) |
| `WithConversationID` | `id string` | auto-generated | Session identifier for state persistence |

## Internal Pipeline

When context management options are set, the SDK inserts two pipeline stages:

1. **ContextAssemblyStage** — runs before the LLM call. Loads summaries, retrieves relevant messages, and loads the hot window. Assembles them into the context sent to the provider.
2. **IncrementalSaveStage** — runs after the LLM call. Appends only the new messages (user + assistant) to the store, and triggers summarization when the threshold is exceeded.

## Production Tips

- **Use Redis** for distributed, persistent state: `statestore.NewRedisStore(redisClient)`. Redis Lists give O(1) append and tail reads.
- **Choose the right embedding model**: `text-embedding-3-small` is fast and cheap; `text-embedding-3-large` is more accurate. Voyage AI (`voyage-3.5`) is recommended for Claude-based systems.
- **Tune the context window** to your use case: 10-20 messages is typical for support; 5-10 for focused Q&A.
- **Set summarization threshold** based on your model's context size and average message length.
- **Use a cheap summarizer**: `gpt-4o-mini` or a similar small model works well for generating summaries.

## See Also

- [Manage Context How-To](/sdk/how-to/manage-context/) — SDK context management options
- [State Store Reference](/runtime/reference/statestore/) — Store interfaces and implementations
