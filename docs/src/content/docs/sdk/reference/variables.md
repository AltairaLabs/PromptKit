---
title: Variable Providers API Reference
sidebar:
  order: 5
---
Complete reference for dynamic variable resolution.

## Provider Interface

```go
type Provider interface {
    Name() string
    Provide(ctx context.Context) (map[string]string, error)
}
```

### Methods

#### Name

```go
func (p Provider) Name() string
```

Returns the provider identifier for logging and debugging.

#### Provide

```go
func (p Provider) Provide(ctx context.Context) (map[string]string, error)
```

Resolves variables dynamically. Called before each template render.

**Returns:**
- `map[string]string`: Variables to inject into template context
- `error`: Error if resolution fails

## SDK Option

### WithVariableProvider

```go
func WithVariableProvider(provider variables.Provider) Option
```

Configures a variable provider for dynamic variable resolution.

**Example:**

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(variables.NewTimeProvider()),
)
```

Multiple providers can be configured:

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(variables.NewTimeProvider()),
    sdk.WithVariableProvider(&myRAGProvider{}),
)
```

## Built-in Providers

### TimeProvider

Injects current time and date information.

```go
func NewTimeProvider() Provider
```

**Variables Provided:**

| Variable | Format | Example |
|----------|--------|---------|
| `current_time` | 15:04:05 | "14:30:45" |
| `current_date` | 2006-01-02 | "2025-01-15" |
| `current_datetime` | 2006-01-02 15:04:05 | "2025-01-15 14:30:45" |
| `day_of_week` | Monday | "Wednesday" |
| `timezone` | Zone name | "America/New_York" |

**Example:**

```go
provider := variables.NewTimeProvider()

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(provider),
)

// In pack template:
// "The current time is {{current_time}} on {{day_of_week}}."
```

### StateProvider

Extracts variables from conversation state metadata.

```go
func NewStateProvider(store statestore.Store, conversationID string) Provider
```

**Parameters:**
- `store`: State store containing conversation metadata
- `conversationID`: ID of the conversation to read from

**Variables Provided:**

Any key-value pairs stored in the conversation's metadata.

**Example:**

```go
store := statestore.NewMemoryStore()
convID := "conv-123"

// Store some metadata
state, _ := store.Load(ctx, convID)
state.Metadata["user_tier"] = "premium"
state.Metadata["language"] = "en"
store.Save(ctx, convID, state)

// Use StateProvider
provider := variables.NewStateProvider(store, convID)

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID(convID),
    sdk.WithVariableProvider(provider),
)

// In pack template:
// "User tier: {{user_tier}}, Language: {{language}}"
```

### ChainProvider

Combines multiple providers in sequence.

```go
func Chain(providers ...Provider) *ChainProvider
```

**Resolution Order:**
1. Providers are called in order
2. Later providers override earlier ones for the same key
3. Errors from any provider stop the chain

**Example:**

```go
chain := variables.Chain(
    variables.NewTimeProvider(),       // Base time variables
    variables.NewStateProvider(store, convID), // User state
    &myRAGProvider{},                  // RAG context
)

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithVariableProvider(chain),
)
```

## Creating Custom Providers

### Basic Provider

```go
type MyProvider struct {
    data map[string]string
}

func (p *MyProvider) Name() string {
    return "my_provider"
}

func (p *MyProvider) Provide(ctx context.Context) (map[string]string, error) {
    return p.data, nil
}
```

### Database Provider

```go
type DatabaseProvider struct {
    db     *sql.DB
    userID string
}

func NewDatabaseProvider(db *sql.DB, userID string) *DatabaseProvider {
    return &DatabaseProvider{db: db, userID: userID}
}

func (p *DatabaseProvider) Name() string {
    return "database"
}

func (p *DatabaseProvider) Provide(ctx context.Context) (map[string]string, error) {
    var name, email string
    err := p.db.QueryRowContext(ctx,
        "SELECT name, email FROM users WHERE id = $1",
        p.userID,
    ).Scan(&name, &email)
    if err != nil {
        return nil, fmt.Errorf("failed to load user: %w", err)
    }

    return map[string]string{
        "user_name":  name,
        "user_email": email,
    }, nil
}
```

### RAG Provider

```go
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
    // Get query from context (set by pipeline)
    query, ok := ctx.Value("current_query").(string)
    if !ok || query == "" {
        return nil, nil
    }

    results, err := p.vectorDB.Search(ctx, query, p.topK)
    if err != nil {
        return nil, err
    }

    var context strings.Builder
    for i, r := range results {
        context.WriteString(fmt.Sprintf("[%d] %s\n", i+1, r.Text))
    }

    return map[string]string{
        "rag_context": context.String(),
        "num_sources": strconv.Itoa(len(results)),
    }, nil
}
```

### API Provider

```go
type WeatherProvider struct {
    apiKey   string
    location string
}

func (p *WeatherProvider) Name() string {
    return "weather"
}

func (p *WeatherProvider) Provide(ctx context.Context) (map[string]string, error) {
    resp, err := http.Get(fmt.Sprintf(
        "https://api.weather.com/v1/current?location=%s&key=%s",
        p.location, p.apiKey,
    ))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var weather struct {
        Temp      float64 `json:"temperature"`
        Condition string  `json:"condition"`
    }
    json.NewDecoder(resp.Body).Decode(&weather)

    return map[string]string{
        "weather_temp":      fmt.Sprintf("%.1f°C", weather.Temp),
        "weather_condition": weather.Condition,
    }, nil
}
```

## Error Handling

### Graceful Degradation

```go
func (p *OptionalProvider) Provide(ctx context.Context) (map[string]string, error) {
    result, err := p.fetchData(ctx)
    if err != nil {
        // Log but don't fail
        log.Printf("Provider %s failed: %v", p.Name(), err)
        return map[string]string{
            "optional_data": "Not available",
        }, nil
    }
    return result, nil
}
```

### Timeout Handling

```go
func (p *SlowProvider) Provide(ctx context.Context) (map[string]string, error) {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    result, err := p.slowOperation(ctx)
    if errors.Is(err, context.DeadlineExceeded) {
        return map[string]string{
            "slow_data": "Loading...",
        }, nil
    }
    if err != nil {
        return nil, err
    }
    return result, nil
}
```

## Resolution Order

When multiple providers are configured:

1. Static variables from `SetVar()` / `SetVars()` are set first
2. Variable providers are called in configuration order
3. Each provider can override variables from previous providers
4. Final variables are used for template rendering

```go
conv.SetVar("greeting", "Hello")  // Static: greeting = "Hello"

// Provider 1: greeting = "Hi", name = "User"
// Provider 2: name = "Alice"

// Final: greeting = "Hi", name = "Alice"
```

## Best Practices

1. **Keep Providers Fast**: Called on every `Send()`, avoid slow operations
2. **Cache Expensive Lookups**: Use TTL-based caching for external calls
3. **Handle Errors Gracefully**: Return defaults instead of failing
4. **Use Timeouts**: Prevent slow providers from blocking
5. **Log Failures**: Track provider issues for debugging
6. **Test Independently**: Unit test providers separately

## See Also

- [Variable Providers Tutorial](../tutorials/09-variable-providers) - Getting started
- [State Management](04-state-management) - Conversation state
- [Template Variables](/runtime/explanation/templates) - Template syntax
