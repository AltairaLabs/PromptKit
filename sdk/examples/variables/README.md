# Variable Providers Example

This example demonstrates the **Variable Providers** feature in PromptKit SDK, which allows dynamic injection of variables into prompts at runtime.

## Features Demonstrated

1. **TimeProvider** - Built-in provider that injects current time/date variables
2. **Custom Provider** - Creating your own provider for application-specific context
3. **Automatic Resolution** - Variables are resolved before each `Send()` call

## Running the Example

```bash
export OPENAI_API_KEY=your-key
cd sdk/examples/variables
go run .
```

## How It Works

### 1. Create Providers

```go
// Built-in time provider
timeProvider := variables.NewTimeProviderWithLocation(time.Local)

// Custom provider for user context
userProvider := NewUserContextProvider("user-12345")
```

### 2. Add to Conversation

```go
conv, err := sdk.Open("./assistant.pack.json", "assistant",
    sdk.WithVariableProvider(timeProvider),
    sdk.WithVariableProvider(userProvider),
)
```

### 3. Variables Auto-Inject

Each time you call `conv.Send()`, all providers are queried and their variables are merged into the template context:

```go
// TimeProvider provides: current_time, current_date, current_weekday, etc.
// UserContextProvider provides: user_id, user_language, response_style, etc.
resp, err := conv.Send(ctx, "What time is it?")
```

## Creating Custom Providers

Implement the `variables.Provider` interface:

```go
type Provider interface {
    Name() string
    Provide(ctx context.Context) (map[string]string, error)
}
```

Example:

```go
type MyProvider struct {
    // your fields
}

func (p *MyProvider) Name() string {
    return "my_provider"
}

func (p *MyProvider) Provide(ctx context.Context) (map[string]string, error) {
    // Fetch data from database, API, cache, etc.
    return map[string]string{
        "my_variable": "my_value",
    }, nil
}
```

## Available Built-in Providers

| Provider | Variables | Description |
|----------|-----------|-------------|
| `TimeProvider` | `current_time`, `current_date`, `current_year`, `current_month`, `current_weekday`, `current_hour` | Current time/date info |
| `StateProvider` | Metadata from conversation state | Extracts variables from StateStore |
| `ChainProvider` | Combined from multiple providers | Composes providers together |

## Use Cases

- **Personalization**: Inject user preferences, language, timezone
- **Context Awareness**: Current time, location, session data
- **A/B Testing**: Inject experiment variants
- **Multi-tenancy**: Inject tenant-specific configuration
- **External Data**: Fetch real-time data from APIs/databases
