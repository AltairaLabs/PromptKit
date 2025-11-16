---
layout: default
title: context-management
parent: SDK Examples
grand_parent: Guides
---

# Context Management Example

This example demonstrates how to use the ContextBuilderMiddleware to manage token budgets and context truncation in conversations.

## What is Context Management?

Context management helps you control:
- **Token costs**: Keep conversations within budget by limiting context size
- **Model limits**: Stay within model context window limits (e.g., GPT-4's 8K/32K/128K token limits)
- **Performance**: Smaller contexts = faster responses and lower latency

## Truncation Strategies

The SDK supports four truncation strategies:

### 1. `TruncateOldest` (Default)
Removes the oldest messages first when the token budget is exceeded.

**Best for**: 
- Customer support chats where recent context is most important
- Task-oriented conversations where early messages become less relevant
- General chatbots where conversation flow matters more than history

**Example**: In a 10-turn conversation, if we exceed the budget, we drop turns 1, 2, 3, etc.

### 2. `TruncateFail`
Returns an error when the token budget is exceeded instead of truncating.

**Best for**:
- Critical conversations where losing context is unacceptable
- Applications with strict compliance requirements
- Cases where you want explicit control over context management

**Example**: If a conversation exceeds 1000 tokens, the SDK returns an error instead of proceeding.

### 3. `TruncateSummarize` (Not shown in example)
Compresses old messages into summaries before removing them.

**Best for**:
- Long conversations where you need to preserve key information
- Research or analysis tasks where history matters
- Use cases where semantic compression is valuable

**Example**: Instead of dropping turn 1 completely, summarize it as "User asked about pricing, got basic plan info"

### 4. `TruncateLeastRelevant` (Not shown in example)
Uses semantic similarity to keep the most relevant messages for the current conversation.

**Best for**:
- Non-linear conversations where topics jump around
- Knowledge retrieval where relevance matters more than recency
- Complex multi-topic discussions

**Example**: If discussing pricing now, keep all pricing-related turns even if they're old, drop irrelevant small talk

## Configuration

```go
contextPolicy := &middleware.ContextBuilderPolicy{
    TokenBudget:      2000,  // Maximum tokens for entire context
    ReserveForOutput: 500,   // Reserve tokens for the model's response
    Strategy:         middleware.TruncateOldest,
    CacheBreakpoints: false, // Enable Anthropic-style cache markers
}

config := sdk.ConversationConfig{
    UserID:        "user-123",
    PromptName:    "assistant",
    ContextPolicy: contextPolicy,  // Pass the policy here
    Variables: map[string]interface{}{
        "name": "Assistant",
    },
}
```

## Token Budget Calculation

The effective budget for conversation history is:
```
Available for history = TokenBudget - ReserveForOutput - SystemPrompt tokens - CurrentMessage tokens
```

For example with a 2000 token budget:
- TokenBudget: 2000
- ReserveForOutput: 500
- SystemPrompt: ~100 tokens
- CurrentMessage: ~50 tokens
- **Available for history**: 2000 - 500 - 100 - 50 = 1350 tokens

## Running the Example

```bash
# Set your OpenAI API key
export OPENAI_API_KEY="your-api-key-here"

# Run the example
go run main.go
```

## Expected Output

The example demonstrates two scenarios:

1. **Oldest-first truncation**: A 5-turn conversation with a 2000 token budget
   - Shows how the SDK automatically removes old messages
   - Conversation continues smoothly despite truncation

2. **Fail-on-overflow**: A conversation with a strict 1000 token budget
   - Shows how the SDK returns an error when budget is exceeded
   - Gives you explicit control over handling overflow

## Production Considerations

1. **Choose the right budget**: Consider your model's limits and typical conversation lengths
2. **Reserve enough for output**: Set ReserveForOutput based on expected response length
3. **Pick the right strategy**: Match the strategy to your use case requirements
4. **Monitor token usage**: Track `result.TokensUsed` to understand actual consumption
5. **Handle errors gracefully**: When using `TruncateFail`, have a fallback strategy

## Cost Optimization Tips

- Use aggressive budgets (e.g., 1000-2000 tokens) for simple Q&A
- Reserve more tokens (e.g., 4000-8000) for complex reasoning tasks
- Consider `TruncateSummarize` for long conversations to preserve context while reducing costs
- Use `TruncateLeastRelevant` when conversation topics are non-linear

## Next Steps

- Experiment with different token budgets
- Try the `TruncateSummarize` strategy for long conversations
- Implement custom truncation logic by extending the middleware
- Monitor token usage in production to optimize budgets
