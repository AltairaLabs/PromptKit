---
title: Manage Context
sidebar:
  order: 5
---
Learn how to configure context management and truncation for long conversations.

## Overview

When conversations grow long, they may exceed the LLM's context window. The SDK provides context management options:

- **Token budget** - Maximum tokens for context
- **Truncation strategy** - How to reduce context when over budget
- **Relevance truncation** - Use embeddings to keep relevant messages

## Set Token Budget

Limit the total tokens used for context:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTokenBudget(8000),
)
```

When the conversation exceeds this budget, older messages are truncated.

## Truncation Strategies

### Sliding Window (Default)

Removes oldest messages first:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTokenBudget(8000),
    sdk.WithTruncation("sliding"),
)
```

### Relevance-Based

Keeps semantically relevant messages using embeddings:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/sdk"
)

// Create embedding provider
embProvider, _ := openai.NewEmbeddingProvider()

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTokenBudget(8000),
    sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
        EmbeddingProvider:   embProvider,
        MinRecentMessages:   3,
        SimilarityThreshold: 0.3,
    }),
)
```

## Long Conversation Context

For applications with persistent state (a state store), you can use a three-tier approach instead of truncation to manage long conversations efficiently:

1. **Hot window** — load only the most recent N messages
2. **Semantic retrieval** — find relevant older messages using embeddings
3. **Auto-summarization** — compress old turns into summaries

These options use `ContextAssemblyStage` and `IncrementalSaveStage` internally, avoiding the need to load and save the full conversation history on every turn.

### Context Window (Hot Window)

Load only the last N messages instead of the full history:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID("session-123"),
    sdk.WithContextWindow(20), // Keep last 20 messages
)
```

Requires `WithStateStore` and `WithConversationID`. When the store implements the optional `MessageReader` and `MessageAppender` interfaces (as `RedisStore` and `MemoryStore` do), only the tail of the conversation is loaded and new messages are appended incrementally. Falls back to full `Load`/`Save` otherwise.

### Semantic Retrieval

On each turn, embed the user's message and search older messages (outside the hot window) for semantic matches:

```go
embProvider, _ := openai.NewEmbeddingProvider()

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID("session-123"),
    sdk.WithContextWindow(20),
    sdk.WithContextRetrieval(embProvider, 5), // Retrieve top 5 matches
)
```

Retrieved messages are inserted chronologically between summaries and the hot window. Requires `WithContextWindow` to be set.

### Auto-Summarization

When the message count exceeds a threshold, compress the oldest unsummarized batch into a summary:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID("session-123"),
    sdk.WithContextWindow(20),
    sdk.WithAutoSummarize(summaryProvider, 50, 10), // threshold=50, batchSize=10
)
```

Summaries are prepended to the context as system messages. You can use a cheaper or faster model for the summary provider (e.g., `gpt-4o-mini`) to minimize cost.

### Combining All Three

Use all three tiers together for the most efficient long conversation handling:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/sdk"
)

store := statestore.NewMemoryStore()
embProvider, _ := openai.NewEmbeddingProvider()
summaryProvider := // your LLM provider for summarization

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID("session-123"),
    sdk.WithContextWindow(20),
    sdk.WithContextRetrieval(embProvider, 5),
    sdk.WithAutoSummarize(summaryProvider, 50, 10),
)
```

## Embedding Providers

### OpenAI

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/openai"

// Default model (text-embedding-3-small)
embProvider, _ := openai.NewEmbeddingProvider()

// Custom model
embProvider, _ := openai.NewEmbeddingProvider(
    openai.WithEmbeddingModel("text-embedding-3-large"),
)
```

### Gemini

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"

// Default model (text-embedding-004)
embProvider, _ := gemini.NewEmbeddingProvider()

// Custom model
embProvider, _ := gemini.NewEmbeddingProvider(
    gemini.WithGeminiEmbeddingModel("embedding-001"),
)
```

### Voyage AI

Recommended by Anthropic for Claude-based systems:

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/voyageai"

// Default model (voyage-3.5)
embProvider, _ := voyageai.NewEmbeddingProvider()

// Code-optimized model
embProvider, _ := voyageai.NewEmbeddingProvider(
    voyageai.WithModel("voyage-code-3"),
)

// With input type hint for retrieval
embProvider, _ := voyageai.NewEmbeddingProvider(
    voyageai.WithInputType(voyageai.InputTypeQuery),
)
```

## RelevanceConfig Options

```go
&sdk.RelevanceConfig{
    // Required: embedding provider
    EmbeddingProvider: embProvider,

    // Always keep N most recent messages (default: 3)
    MinRecentMessages: 3,

    // Never truncate system messages (default: true)
    AlwaysKeepSystemRole: true,

    // Minimum similarity score to keep (default: 0.0)
    SimilarityThreshold: 0.3,

    // What to compare messages against (default: "last_user")
    QuerySource: "last_user",  // or "last_n", "custom"

    // For QuerySource "last_n": how many messages
    LastNCount: 3,

    // For QuerySource "custom": the query text
    CustomQuery: "customer billing issue",
}
```

## Query Source Options

Control what the relevance is computed against:

### Last User Message (Default)

Compare against the most recent user message:

```go
&sdk.RelevanceConfig{
    EmbeddingProvider: embProvider,
    QuerySource:       "last_user",
}
```

### Last N Messages

Compare against multiple recent messages:

```go
&sdk.RelevanceConfig{
    EmbeddingProvider: embProvider,
    QuerySource:       "last_n",
    LastNCount:        5,
}
```

### Custom Query

Compare against a specific query:

```go
&sdk.RelevanceConfig{
    EmbeddingProvider: embProvider,
    QuerySource:       "custom",
    CustomQuery:       "technical support for billing",
}
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    ctx := context.Background()

    // Create embedding provider for relevance truncation
    embProvider, err := openai.NewEmbeddingProvider()
    if err != nil {
        log.Fatal(err)
    }

    // Open conversation with context management
    conv, err := sdk.Open("./app.pack.json", "chat",
        sdk.WithTokenBudget(8000),
        sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
            EmbeddingProvider:    embProvider,
            MinRecentMessages:    3,
            SimilarityThreshold:  0.3,
            AlwaysKeepSystemRole: true,
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Simulate a long conversation
    messages := []string{
        "I'm having trouble with my account billing",
        "The charge appeared on December 15th",
        "What's the weather like today?",  // Unrelated - may be truncated
        "Can you look up my order history?",
        "Back to my billing - can you help fix it?",
    }

    for _, msg := range messages {
        resp, err := conv.Send(ctx, msg)
        if err != nil {
            log.Fatal(err)
        }
        fmt.Printf("User: %s\nAssistant: %s\n\n", msg, resp.Text())
    }
}
```

## Performance Tips

1. **Cache embeddings**: Enabled by default in RelevanceConfig
2. **Use smaller models**: `text-embedding-3-small` is faster than `large`
3. **Set appropriate threshold**: Higher threshold = fewer messages = faster
4. **Batch requests**: The SDK batches embedding requests automatically

## Environment Variables

Set API keys for embedding providers:

```bash
# OpenAI
export OPENAI_API_KEY=sk-...

# Gemini
export GEMINI_API_KEY=...

# Voyage AI
export VOYAGE_API_KEY=...
```

## See Also

- [Open a Conversation](initialize) - Basic setup
- [Manage Variables](manage-state) - Template variables
- [Long Conversation Example](../../../sdk/examples/long-conversation/) - Full working demo
- [Arena Context Management](../../arena/how-to/manage-context) - YAML configuration
