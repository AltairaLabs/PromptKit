---
title: "Tutorial 9: Variable Providers"
sidebar:
  order: 9
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/pipeline/stage/stages_utilities.go
    - runtime/statestore/memory.go
    - runtime/statestore/types.go
    - runtime/variables/chain.go
    - runtime/variables/provider.go
    - runtime/variables/request.go
    - runtime/variables/state.go
    - runtime/variables/time.go
    - sdk/conversation.go
    - sdk/json_input.go
    - sdk/options.go
    - sdk/session_vars.go
---
In this tutorial we inject dynamic context into prompts with variable providers. A variable provider resolves template variables at runtime, before the pipeline renders the prompt. We use the built-in time, state and chain providers, then write custom providers for RAG and database lookups.

## Prerequisites

- Completed [Tutorial 1: First Conversation](/sdk/tutorials/01-first-conversation/)
- Understanding of template variables

## Register a provider

A provider implements `variables.Provider`, and `sdk.WithVariableProvider` registers it. The pipeline calls every provider on each `Send()`.

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(&myRAGProvider{}),
)
```

For a value you set yourself, `conv.SetVar` stores it on the conversation. The next `Send()` renders it, and it overrides a provider's value for the same name.

```go
conv.SetVar("user_name", "Alice")
```

## Built-in providers

### TimeProvider

`variables.NewTimeProvider()` injects the current time and date:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/variables"

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

`variables.NewStateProvider` turns the metadata saved in conversation state into variables:

```go
import (
    "context"

    "github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

store := statestore.NewMemoryStore()
conversationID := "conv-1"

// Save metadata for the conversation.
_ = store.Save(context.Background(), &statestore.ConversationState{
    ID:       conversationID,
    Metadata: map[string]interface{}{"user_tier": "premium"},
})

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID(conversationID),
    sdk.WithVariableProvider(variables.NewStateProvider(store, conversationID)),
)

// {{user_tier}} resolves to "premium"
```

### Chain

`variables.Chain` combines providers into one. It calls them in order, and a later provider overrides an earlier one on the same key:

```go
chain := variables.Chain(
    variables.NewTimeProvider(),
    variables.NewStateProvider(store, conversationID),
    &myRAGProvider{},
)

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(chain),
)
```

## Creating a custom provider

A custom provider implements two methods:

```go
type Provider interface {
    Name() string
    Provide(ctx context.Context) (map[string]string, error)
}
```

### A RAG provider

The provider reads the query from the per-send variables on `ctx`. The caller binds them with `sdk.WithJSONInput`, shown after the pack.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strings"

    "github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

type SearchResult struct {
    Text string
}

type VectorDatabase interface {
    Search(ctx context.Context, query string, topK int) ([]SearchResult, error)
}

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
    query := variables.RequestVars(ctx)["query"]
    if query == "" {
        return map[string]string{"rag_context": "No additional context available."}, nil
    }

    results, err := p.vectorDB.Search(ctx, query, p.topK)
    if err != nil {
        // Log the error and keep the conversation going.
        log.Printf("RAG search failed: %v", err)
        return map[string]string{"rag_context": "No additional context available."}, nil
    }

    var b strings.Builder
    for i, result := range results {
        fmt.Fprintf(&b, "[%d] %s\n", i+1, result.Text)
    }
    return map[string]string{"rag_context": b.String()}, nil
}
```

If `Provide` returns an error, the `Send()` fails. This provider returns a placeholder instead, so a search outage does not end the conversation.

### Using the RAG provider

The pack's system template references the variable:

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
      "system_template": "You are a support agent. Use the following context to answer questions:\n\n{{rag_context}}"
    }
  }
}
```

The Go code registers the provider and passes the query with each send:

```go
ragProvider := NewRAGProvider(myVectorDB, 5)

conv, _ := sdk.Open("./rag-assistant.pack.json", "support",
    sdk.WithVariableProvider(ragProvider),
)

resp, _ := conv.Send(ctx, "How do I reset my password?",
    sdk.WithJSONInput(map[string]any{"query": "reset password"}),
)
```

The pipeline calls `Provide` on each `Send()`, so `{{rag_context}}` follows the query.

### A database provider

A provider can read from any source. This one loads the signed-in user's profile:

```go
type UserDataProvider struct {
    db     *sql.DB
    userID string
}

func (p *UserDataProvider) Name() string {
    return "user_data"
}

func (p *UserDataProvider) Provide(ctx context.Context) (map[string]string, error) {
    var name, tier string
    err := p.db.QueryRowContext(ctx,
        "SELECT name, tier FROM users WHERE id = $1", p.userID,
    ).Scan(&name, &tier)
    if err != nil {
        return nil, err
    }

    return map[string]string{
        "user_name": name,
        "user_tier": tier,
    }, nil
}
```

### A provider with a timeout

`Provide` receives the request context. Wrap it in a deadline to bound a slow source:

```go
func (p *SlowProvider) Provide(ctx context.Context) (map[string]string, error) {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    result, err := p.slowOperation(ctx)
    if errors.Is(err, context.DeadlineExceeded) {
        return map[string]string{"external_data": "Loading..."}, nil
    }
    if err != nil {
        return nil, err
    }
    return map[string]string{"external_data": result}, nil
}
```

### A conditional provider

A wrapper can skip a provider when a condition fails. Returning `nil, nil` adds no variables:

```go
type ConditionalProvider struct {
    condition func(ctx context.Context) bool
    provider  variables.Provider
}

func (p *ConditionalProvider) Name() string {
    return "conditional(" + p.provider.Name() + ")"
}

func (p *ConditionalProvider) Provide(ctx context.Context) (map[string]string, error) {
    if !p.condition(ctx) {
        return nil, nil
    }
    return p.provider.Provide(ctx)
}
```

## Putting it together

This program chains the time provider, the user provider and the RAG provider:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/variables"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    chain := variables.Chain(
        variables.NewTimeProvider(),
        &UserDataProvider{db: myDB, userID: "user-123"},
        NewRAGProvider(myVectorDB, 3),
    )

    conv, err := sdk.Open("./support.pack.json", "agent",
        sdk.WithVariableProvider(chain),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    resp, err := conv.Send(context.Background(), "What's my account status?",
        sdk.WithJSONInput(map[string]any{"query": "account status"}),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

Each `Send()` resolves the three providers in order. A `conv.SetVar` value or a `WithJSONInput` binding overrides a provider's value for the same name.

## Testing a provider

A provider is a plain Go type, so we test it without a conversation. Run it with `go test`; it passes. Pass the per-send variables on the context with `variables.WithRequestVars`:

```go
func TestRAGProvider(t *testing.T) {
    mockDB := &mockVectorDB{
        results: []SearchResult{
            {Text: "Password reset: Go to settings..."},
        },
    }

    provider := NewRAGProvider(mockDB, 3)

    ctx := variables.WithRequestVars(context.Background(),
        map[string]string{"query": "reset password"})
    vars, err := provider.Provide(ctx)

    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(vars["rag_context"], "Password reset") {
        t.Errorf("rag_context = %q", vars["rag_context"])
    }
}
```

## Next steps

- [How-to guides](/sdk/how-to/): task-specific guides
- [API reference](/sdk/reference/): complete API documentation
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples): working code
- [Variable providers reference](/runtime/reference/variables/): complete API
- [State management tutorial](/sdk/tutorials/04-state-management/): conversation state
