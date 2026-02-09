---
title: "Tutorial 9: Variable Providers"
sidebar:
  order: 9
---
Inject dynamic context into prompts with variable providers.

## What You'll Learn

- What variable providers are and when to use them
- Built-in providers: Time, State, Chain
- Creating custom providers for RAG and database lookups
- Chaining multiple providers together

## Prerequisites

- Completed [Tutorial 1: First Conversation](01-first-conversation)
- Understanding of template variables

## What Are Variable Providers?

Variable providers dynamically resolve variables at runtime, before template rendering. They're useful for:

- **RAG**: Injecting context from vector search
- **Session State**: Including user preferences or history summaries
- **Time-sensitive Data**: Current timestamps, business hours
- **External APIs**: Live data from databases or services

### Static vs Dynamic Variables

```go
// Static: Set once at conversation start
conv.SetVar("user_name", "Alice")

// Dynamic: Resolved on each Send()
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(&myRAGProvider{}),
)
```

## Built-in Providers

### TimeProvider

Injects current time and date information:

```go
import "github.com/AltairaLabs/PromptKit/runtime/variables"

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(variables.NewTimeProvider()),
)

// Available variables:
// {{current_time}}    - "2025-01-15T15:04:05Z" (RFC3339 format)
// {{current_date}}    - "2025-01-15"
// {{current_year}}    - "2025"
// {{current_month}}   - "January"
// {{current_weekday}} - "Wednesday"
// {{current_hour}}    - "15"
```

### StateProvider

Extracts variables from conversation state metadata:

```go
import "github.com/AltairaLabs/PromptKit/runtime/variables"

// StateProvider reads from statestore metadata
stateProvider := variables.NewStateProvider(stateStore, conversationID)

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithStateStore(stateStore),
    sdk.WithConversationID(conversationID),
    sdk.WithVariableProvider(stateProvider),
)

// Any metadata saved to state is available as variables
// e.g., if state has metadata["user_tier"] = "premium"
// then {{user_tier}} resolves to "premium"
```

### ChainProvider

Combines multiple providers in sequence:

```go
import "github.com/AltairaLabs/PromptKit/runtime/variables"

chain := variables.Chain(
    variables.NewTimeProvider(),
    variables.NewStateProvider(store, convID),
    &myRAGProvider{},
)

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(chain),
)

// Variables resolved in order; later providers override earlier ones
```

## Creating Custom Providers

### Provider Interface

```go
type Provider interface {
    Name() string
    Provide(ctx context.Context) (map[string]string, error)
}
```

### Example: RAG Provider

```go
package main

import (
    "context"
    "github.com/AltairaLabs/PromptKit/runtime/variables"
)

type RAGProvider struct {
    vectorDB VectorDatabase
    topK     int
}

func NewRAGProvider(db VectorDatabase, topK int) *RAGProvider {
    return &RAGProvider{vectorDB: db, topK: topK}
}

func (p *RAGProvider) Name() string {
    return "rag"
}

func (p *RAGProvider) Provide(ctx context.Context) (map[string]string, error) {
    // Get the current query from context (set by the pipeline)
    query := ctx.Value("current_query").(string)

    // Search vector database
    results, err := p.vectorDB.Search(ctx, query, p.topK)
    if err != nil {
        return nil, err
    }

    // Format results as context
    var context strings.Builder
    for i, result := range results {
        context.WriteString(fmt.Sprintf("[%d] %s\n", i+1, result.Text))
    }

    return map[string]string{
        "rag_context": context.String(),
        "rag_sources": formatSources(results),
    }, nil
}
```

### Using the RAG Provider

Pack file with RAG context:

```json
{
  "id": "rag-assistant",
  "name": "RAG Assistant",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "support": {
      "id": "support",
      "name": "Support Agent",
      "version": "1.0.0",
      "system_template": "You are a support agent. Use the following context to answer questions:\n\n{{rag_context}}\n\nSources: {{rag_sources}}"
    }
  }
}
```

Go code:

```go
ragProvider := NewRAGProvider(myVectorDB, 5)

conv, _ := sdk.Open("./rag-assistant.pack.json", "support",
    sdk.WithVariableProvider(ragProvider),
)

// RAG context automatically injected on each Send()
resp, _ := conv.Send(ctx, "How do I reset my password?")
```

### Example: Database Provider

```go
type UserDataProvider struct {
    db     *sql.DB
    userID string
}

func (p *UserDataProvider) Name() string {
    return "user_data"
}

func (p *UserDataProvider) Provide(ctx context.Context) (map[string]string, error) {
    var user struct {
        Name        string
        Tier        string
        LastLogin   time.Time
        Preferences string
    }

    err := p.db.QueryRowContext(ctx,
        "SELECT name, tier, last_login, preferences FROM users WHERE id = $1",
        p.userID,
    ).Scan(&user.Name, &user.Tier, &user.LastLogin, &user.Preferences)
    if err != nil {
        return nil, err
    }

    return map[string]string{
        "user_name":        user.Name,
        "user_tier":        user.Tier,
        "last_login":       user.LastLogin.Format("2006-01-02"),
        "user_preferences": user.Preferences,
    }, nil
}
```

## Provider Chaining

### Order Matters

Variables are resolved in order, with later providers overriding earlier ones:

```go
chain := variables.Chain(
    &defaultsProvider{},     // Base defaults
    &userDataProvider{},     // User-specific data (overrides defaults)
    &ragProvider{},          // RAG context
)
```

### Conditional Providers

```go
type ConditionalProvider struct {
    condition func(ctx context.Context) bool
    provider  variables.Provider
}

func (p *ConditionalProvider) Provide(ctx context.Context) (map[string]string, error) {
    if !p.condition(ctx) {
        return nil, nil // Skip this provider
    }
    return p.provider.Provide(ctx)
}
```

## Error Handling

### Graceful Degradation

```go
func (p *RAGProvider) Provide(ctx context.Context) (map[string]string, error) {
    results, err := p.vectorDB.Search(ctx, query, p.topK)
    if err != nil {
        // Log error but don't fail the conversation
        log.Printf("RAG search failed: %v", err)
        return map[string]string{
            "rag_context": "No additional context available.",
        }, nil
    }
    // ... format results
}
```

### Timeout Handling

```go
func (p *SlowProvider) Provide(ctx context.Context) (map[string]string, error) {
    // Create timeout context
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    result, err := p.slowOperation(ctx)
    if errors.Is(err, context.DeadlineExceeded) {
        return map[string]string{
            "external_data": "Loading...",
        }, nil
    }
    // ...
}
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/variables"
)

func main() {
    // Create provider chain
    chain := variables.Chain(
        // Time context
        variables.NewTimeProvider(),

        // User data from database
        &UserDataProvider{db: myDB, userID: "user-123"},

        // RAG context from vector search
        &RAGProvider{vectorDB: myVectorDB, topK: 3},
    )

    // Open conversation with providers
    conv, err := sdk.Open("./support.pack.json", "agent",
        sdk.WithVariableProvider(chain),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Variables automatically resolved on each Send()
    resp, err := conv.Send(context.Background(), "What's my account status?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

## Testing Providers

```go
func TestRAGProvider(t *testing.T) {
    // Create mock vector DB
    mockDB := &mockVectorDB{
        results: []SearchResult{
            {Text: "Password reset: Go to settings...", Score: 0.95},
        },
    }

    provider := NewRAGProvider(mockDB, 3)

    ctx := context.WithValue(context.Background(), "current_query", "reset password")
    vars, err := provider.Provide(ctx)

    assert.NoError(t, err)
    assert.Contains(t, vars["rag_context"], "Password reset")
}
```

## Best Practices

1. **Keep Providers Fast**: Providers run on every `Send()`, keep them quick
2. **Cache When Possible**: Cache expensive lookups with TTLs
3. **Handle Errors Gracefully**: Don't fail conversations for optional context
4. **Use Timeouts**: Prevent slow providers from blocking
5. **Log Failures**: Track provider issues for debugging
6. **Test Independently**: Unit test providers separately from conversations

## What's Next

You've completed the SDK tutorials! Explore:

- [How-To Guides](../how-to/) - Task-specific guides
- [API Reference](../reference/) - Complete API documentation
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples) - Working code examples

## See Also

- [Variable Providers Reference](../reference/variables) - Complete API
- [State Management Tutorial](04-state-management) - Conversation state
