---
title: Context Management Example
description: Example demonstrating context-management
sidebar:
  order: 100
---
This example demonstrates Arena's context management capabilities for handling long conversations with token budget constraints.

## Overview

Context management prevents conversations from exceeding provider token limits by intelligently truncating or managing message history. This is critical for:

- **Long conversations**: 20+ turn conversations
- **Cost optimization**: Reduce tokens sent to provider
- **Provider limits**: Respect context window sizes (GPT-4: 128k, Claude: 200k)
- **Realistic testing**: Test behavior under production constraints

## Configuration

Context management is configured in the scenario YAML:

```yaml
context_policy:
  token_budget: 50000        # Max tokens for entire context
  reserve_for_output: 4000   # Reserve tokens for response
  strategy: "oldest"         # Truncation strategy
  cache_breakpoints: true    # Enable prompt caching (Anthropic)
```

### Token Budget

- `token_budget`: Maximum tokens for full context (system prompt + messages)
- `reserve_for_output`: Tokens reserved for model response
- Available for messages: `token_budget - reserve_for_output - system_prompt_tokens`

### Truncation Strategies

1. **`oldest`** (default): Drop oldest messages first
   - Simple and predictable
   - Keeps recent context
   - Best for most use cases

2. **`fail`**: Error if budget exceeded
   - Strict mode for testing
   - Ensures no data loss
   - Good for validation

3. **`summarize`** (future): Compress old messages
   - Uses LLM to create summaries
   - Preserves more information
   - Higher latency

4. **`relevance`** (future): Drop least relevant messages
   - Uses embeddings for relevance scoring
   - Keeps important context
   - Requires embedding model

### Cache Breakpoints (Anthropic Only)

When `cache_breakpoints: true`, Arena inserts cache markers for Anthropic's prompt caching:

- System prompt is marked for caching
- Subsequent turns reuse cached prompt
- **90% cost reduction** on cached tokens
- Only works with Anthropic Claude models

## Scenarios

### 1. Unlimited Context (Baseline)

**File**: `scenarios/context-unlimited.yaml`

No context policy = unlimited context (backward compatible).

**Purpose**: Baseline to compare against limited scenarios.

```yaml
# No context_policy specified
turns:
  - role: user
    content: "First message..."
  # ... many turns
  - role: user
    content: "What did I first ask?"  # Full context available
```

### 2. Limited with Oldest Strategy

**File**: `scenarios/context-limited-oldest.yaml`

Very low budget (500 tokens) to force truncation.

**Purpose**: Verify oldest messages are dropped when over budget.

**Expected behavior**:
- Early turns (1-4) are dropped
- Recent turns (5-7) are kept
- Last turn asking "What's my name?" should fail (name was in turn 1)

### 3. Fail on Budget Exceeded

**File**: `scenarios/context-limited-fail.yaml`

Strict mode with `strategy: "fail"`.

**Purpose**: Verify execution errors when budget exceeded.

**Expected behavior**:
- First few turns succeed
- Later turn triggers error: "token budget exceeded"
- No truncation occurs

### 4. With Caching (Anthropic)

**File**: `scenarios/context-with-caching.yaml`

Enable Anthropic prompt caching with `cache_breakpoints: true`.

**Purpose**: Verify cache breakpoints reduce costs.

**Expected behavior**:
- Turn 1: Full cost (cache miss)
- Turn 2+: Reduced cost (cache hit on system prompt)
- Cost breakdown shows cached tokens

## Running the Example

```bash
# Run all scenarios
cd examples/context-management
promptarena run arena.yaml

# Run specific scenario
promptarena run arena.yaml --scenario context-limited-oldest

# Run with specific provider
promptarena run arena.yaml --provider anthropic-claude-sonnet
```

## Expected Output

### Unlimited Context

```
Scenario: context-unlimited
✓ Turn 1: "Tell me about the solar system"
✓ Turn 2: "What are the inner planets?"
...
✓ Turn 5: "What did I first ask you about?"
  Response: "You first asked about the solar system."
  Context: 5/5 messages kept
  Cost: $0.0234
```

### Limited with Oldest Strategy

```
Scenario: context-limited-oldest
⚠ Turn 1-4: Dropped (over budget)
✓ Turn 5: "What about Saturn's rings?"
✓ Turn 6: "Which planet has the most moons?"
✓ Turn 7: "What's my name again?"
  Response: "I don't have that information in our conversation."
  Context: 3/7 messages kept (4 dropped)
  Cost: $0.0089 (62% reduction)
```

### Fail on Budget Exceeded

```
Scenario: context-limited-fail
✓ Turn 1: "Tell me about the solar system"
✓ Turn 2: "What are all the planets?"
✗ Turn 3: Error - token budget exceeded: have 387, budget 300
  Context: Failed before execution
  Cost: $0.0056
```

### With Caching

```
Scenario: context-with-caching
✓ Turn 1: "What are the planets?"
  Cost: $0.0124 (0 cached)
✓ Turn 2: "Tell me about Mercury"
  Cost: $0.0018 (1,234 cached) - 85% reduction
✓ Turn 3: "What about Venus?"
  Cost: $0.0019 (1,234 cached) - 85% reduction
...
Total Cost: $0.0234 (avg 73% reduction from caching)
```

## Implementation Details

### How It Works

1. **Configuration**: Scenario specifies `context_policy`
2. **Pipeline Integration**: Context middleware inserted before Provider middleware
3. **Token Counting**: Simple word-based estimator (words * 1.3)
4. **Truncation**: Applied before each turn execution
5. **Metadata**: Truncation info stored in execution context

### Pipeline Order

```
Template Middleware
    ↓
Context Middleware (NEW) ← Token budget enforcement
    ↓
Provider Middleware
    ↓
Validator Middleware
```

### Token Counting

Current implementation uses simple word-based estimation:
- Split text into words
- Multiply by 1.3 (accounts for subword tokens)
- **Not accurate** but good enough for testing

For production, use:
- `tiktoken` for OpenAI models
- Anthropic tokenizer for Claude
- Provider-specific tokenizers

## Observability

Context truncation is tracked in execution metadata:

```go
execCtx.Metadata["context_truncated"] = true
execCtx.Metadata["context_original_count"] = 7
execCtx.Metadata["context_truncated_count"] = 3
execCtx.Metadata["context_dropped_count"] = 4
```

This appears in Arena output:

```
Context Management:
  Original: 7 messages
  Kept: 3 messages
  Dropped: 4 messages
  Strategy: oldest
  Budget: 500 tokens
```

## Cost Comparison

Expected cost differences:

| Scenario | Tokens | Cost | vs Unlimited |
|----------|--------|------|--------------|
| Unlimited | ~2,500 | $0.0234 | baseline |
| Limited (oldest) | ~950 | $0.0089 | -62% |
| With Caching (turn 1) | 1,234 | $0.0124 | -47% |
| With Caching (turn 2+) | 145 + 1,234 cached | $0.0018 | -92% |

## Future Enhancements

1. **Summarization Strategy**: Use LLM to compress old messages
2. **Relevance Strategy**: Use embeddings to keep relevant messages
3. **Accurate Token Counting**: Use tiktoken for OpenAI
4. **Per-Turn Budget**: Override budget for specific turns
5. **Dynamic Budget**: Adjust based on response needs

## Testing Context Management

Use this example to test:

1. **Truncation Logic**: Does oldest strategy work correctly?
2. **Budget Enforcement**: Does fail strategy error appropriately?
3. **Cost Reduction**: Do limited scenarios save money?
4. **Caching**: Does Anthropic caching reduce costs?
5. **Context Loss**: Do models handle missing context gracefully?
