---
title: Manage Context
sidebar:
  order: 8
---
Learn how to configure context management and truncation strategies for long conversations.

## Overview

When conversations grow long, they may exceed the LLM's context window or token budget. PromptKit provides context management strategies to handle this automatically:

- **Truncate oldest messages** - Remove earliest messages first (simple, fast)
- **Truncate by relevance** - Use embeddings to keep semantically relevant messages (smarter)

## Basic Configuration

Add `context_policy` to your scenario:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: long-conversation-test

spec:
  task_type: support

  context_policy:
    token_budget: 8000
    strategy: truncate_oldest

  turns:
    - role: user
      content: "Start of conversation..."
    # ... many turns ...
```

## Truncation Strategies

### Truncate Oldest (Default)

Removes the oldest messages when approaching the token budget:

```yaml
context_policy:
  token_budget: 8000
  strategy: truncate_oldest
```

**Pros:** Fast, predictable
**Cons:** May remove contextually important early messages

### Relevance-Based Truncation

Uses embedding similarity to keep the most relevant messages:

```yaml
context_policy:
  token_budget: 8000
  strategy: relevance
  relevance:
    provider: openai
    model: text-embedding-3-small
    min_recent_messages: 3
    similarity_threshold: 0.3
```

**Pros:** Preserves semantically important context
**Cons:** Requires embedding API calls (additional latency/cost)

## Relevance Configuration

### Provider Options

Choose an embedding provider:

```yaml
# OpenAI (recommended for general use)
relevance:
  provider: openai
  model: text-embedding-3-small  # or text-embedding-3-large

# Gemini
relevance:
  provider: gemini
  model: text-embedding-004

# Voyage AI (recommended for retrieval tasks)
relevance:
  provider: voyageai
  model: voyage-3.5  # or voyage-code-3 for code
```

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `provider` | string | required | Embedding provider: `openai`, `gemini`, `voyageai` |
| `model` | string | provider default | Embedding model to use |
| `min_recent_messages` | int | `3` | Always keep N most recent messages |
| `similarity_threshold` | float | `0.3` | Minimum similarity score (0-1) to keep message |
| `always_keep_system` | bool | `true` | Never truncate system messages |
| `cache_embeddings` | bool | `true` | Cache embeddings for performance |

### Example: Code Assistant

For code-related conversations, use a code-optimized model:

```yaml
context_policy:
  token_budget: 16000
  strategy: relevance
  relevance:
    provider: voyageai
    model: voyage-code-3
    min_recent_messages: 5
    similarity_threshold: 0.25
```

### Example: Customer Support

For support conversations, preserve context about the customer's issue:

```yaml
context_policy:
  token_budget: 8000
  strategy: relevance
  relevance:
    provider: openai
    model: text-embedding-3-small
    min_recent_messages: 3
    similarity_threshold: 0.35
    always_keep_system: true
```

## How Relevance Truncation Works

1. **Compute query embedding** - Embeds the most recent user message(s)
2. **Score all messages** - Computes cosine similarity between query and each message
3. **Apply rules**:
   - Always keep system messages (if `always_keep_system: true`)
   - Always keep last N messages (per `min_recent_messages`)
   - Keep messages with similarity >= threshold
4. **Truncate remaining** - Remove lowest-scoring messages until under budget

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

## Complete Example

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: extended-support-conversation
  labels:
    category: support
    context: managed

spec:
  task_type: support
  description: "Tests context management in long conversations"

  context_policy:
    token_budget: 8000
    strategy: relevance
    relevance:
      provider: openai
      model: text-embedding-3-small
      min_recent_messages: 3
      similarity_threshold: 0.3

  turns:
    - role: user
      content: "I'm having trouble with my account billing"

    - role: user
      content: "The charge appeared on December 15th"

    - role: user
      content: "What's the weather like today?"
      # This unrelated message may be truncated

    - role: user
      content: "Back to my billing issue - can you help?"
      assertions:
        - type: content_includes
          params:
            patterns: ["billing", "charge", "December"]
            message: "Should remember billing context"
```

## Performance Considerations

- **Caching**: Enable `cache_embeddings: true` to avoid re-computing embeddings
- **Model size**: Smaller models (e.g., `text-embedding-3-small`) are faster
- **Batch size**: Embeddings are computed in batches for efficiency
- **Token budget**: Set appropriately for your LLM's context window

## Troubleshooting

### Missing API Key

```
Error: openai API key not found: set OPENAI_API_KEY environment variable
```

Set the required environment variable for your embedding provider.

### High Latency

If truncation is slow:
1. Enable embedding caching
2. Use a smaller embedding model
3. Increase `similarity_threshold` to keep fewer messages

### Context Lost

If important context is being truncated:
1. Lower `similarity_threshold`
2. Increase `min_recent_messages`
3. Ensure `always_keep_system: true`

## See Also

- [Write Scenarios](write-scenarios) - Scenario configuration basics
- [Configure Providers](configure-providers) - Provider setup
- [SDK Context Management](../../sdk/how-to/manage-context) - Programmatic context control
