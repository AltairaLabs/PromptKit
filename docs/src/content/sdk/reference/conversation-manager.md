---
title: ConversationManager
docType: reference
order: 1
---
# ConversationManager

High-level API for managing LLM conversations with automatic pipeline construction.

## Overview

`ConversationManager` is the primary entry point for building LLM applications with the SDK. It provides:

- PromptPack loading and management
- Automatic pipeline construction
- Multi-turn conversation handling
- State persistence
- Tool execution support
- Streaming capabilities

## Type Definition

```go
type ConversationManager struct {
    // Unexported fields
}
```

## Constructor

### NewConversationManager

Creates a new ConversationManager with the specified options.

```go
func NewConversationManager(opts ...ManagerOption) (*ConversationManager, error)
```

**Parameters:**
- `opts` - Variable number of `ManagerOption` functions to configure the manager

**Returns:**
- `*ConversationManager` - The configured manager instance
- `error` - Error if configuration is invalid

**Example:**

```go
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(redisStore),
    sdk.WithToolRegistry(toolRegistry),
)
if err != nil {
    log.Fatal(err)
}
```

## Methods

### LoadPack

Loads a PromptPack from a .pack.json file.

```go
func (cm *ConversationManager) LoadPack(packPath string) (*Pack, error)
```

**Parameters:**
- `packPath` - Path to the .pack.json file

**Returns:**
- `*Pack` - The loaded pack
- `error` - Error if pack cannot be loaded or validated

**Example:**

```go
pack, err := manager.LoadPack("./support.pack.json")
if err != nil {
    log.Fatalf("Failed to load pack: %v", err)
}
```

### NewConversation

Creates a new conversation from a pack and configuration.

```go
func (cm *ConversationManager) NewConversation(
    ctx context.Context,
    pack *Pack,
    config ConversationConfig,
) (*Conversation, error)
```

**Parameters:**
- `ctx` - Context for the operation
- `pack` - The PromptPack to use
- `config` - Conversation configuration

**Returns:**
- `*Conversation` - The new conversation instance
- `error` - Error if conversation cannot be created

**Example:**

```go
conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "support",
    Variables: map[string]interface{}{
        "role": "customer support agent",
        "language": "English",
    },
    ContextPolicy: &middleware.ContextBuilderPolicy{
        MaxInputTokens: 8000,
        Strategy:       middleware.StrategyTruncateOldest,
    },
})
```

### GetConversation

Retrieves an existing conversation by ID.

```go
func (cm *ConversationManager) GetConversation(
    ctx context.Context,
    conversationID string,
) (*Conversation, error)
```

**Parameters:**
- `ctx` - Context for the operation
- `conversationID` - The conversation ID to retrieve

**Returns:**
- `*Conversation` - The conversation instance
- `error` - Error if conversation not found or cannot be loaded

**Example:**

```go
conv, err := manager.GetConversation(ctx, "conv-abc123")
if err != nil {
    log.Printf("Conversation not found: %v", err)
    return
}

// Continue conversation
resp, _ := conv.Send(ctx, "What was my last question?")
```

## Configuration Types

### ManagerConfig

Configuration for the ConversationManager.

```go
type ManagerConfig struct {
    MaxConcurrentExecutions  int           // Limit parallel pipeline executions
    DefaultTimeout           time.Duration // Default timeout for LLM requests
    EnableMetrics            bool          // Enable built-in metrics collection
    EnableMediaExternalization bool        // Enable media storage (auto-set by WithMediaStorage)
    MediaSizeThresholdKB     int64         // Minimum size to externalize (default: 100)
    MediaDefaultPolicy       string        // Media retention policy (default: "retain")
}
```

**Fields:**

- **MaxConcurrentExecutions** (default: 10)
  - Maximum number of concurrent pipeline executions
  - Prevents resource exhaustion under high load
  - Set to 0 for unlimited

- **DefaultTimeout** (default: 30s)
  - Default timeout for LLM API requests
  - Can be overridden per-message via context
  - Set to 0 for no timeout

- **EnableMetrics** (default: false)
  - Enables built-in metrics collection
  - Tracks latency, token usage, costs
  - Access via manager.GetMetrics()

- **EnableMediaExternalization** (default: false)
  - Automatically set to `true` when using `WithMediaStorage()`
  - Can be explicitly disabled if needed
  - Requires media storage service to be configured

- **MediaSizeThresholdKB** (default: 100)
  - Minimum media size (in KB) to externalize
  - Media smaller than this stays in memory
  - Set to 0 to externalize all media
  - Only applies when `EnableMediaExternalization` is true

- **MediaDefaultPolicy** (default: "retain")
  - Media retention policy: "retain" or "delete"
  - "retain": Keep media files indefinitely
  - "delete": Delete media when conversation ends (not yet implemented)
  - Applies to all conversations unless overridden

### ConversationConfig

Configuration for creating a new conversation.

```go
type ConversationConfig struct {
    // Required
    UserID     string // User who owns this conversation
    PromptName string // Task type from the pack (e.g., "support", "sales")

    // Optional
    ConversationID string                 // If empty, auto-generated
    Variables      map[string]interface{} // Template variables
    SystemPrompt   string                 // Override system prompt
    Metadata       map[string]interface{} // Custom metadata
    ContextPolicy  *middleware.ContextBuilderPolicy // Token budget management
}
```

**Required Fields:**

- **UserID**
  - Identifies the user owning this conversation
  - Used for state persistence and multi-tenancy
  - Must be unique per user

- **PromptName**
  - The task type from the pack to use
  - Must exist in the pack's prompts map
  - Example: "support", "sales", "analyst"

**Optional Fields:**

- **ConversationID**
  - Specify custom conversation ID
  - If empty, auto-generated (UUID)
  - Useful for integrating with external systems

- **Variables**
  - Template variables to inject into prompts
  - Merged with pack defaults
  - Example: `{"role": "agent", "lang": "en"}`

- **SystemPrompt**
  - Override the pack's system prompt
  - Use sparingly; pack prompts are tested
  - Useful for A/B testing

- **Metadata**
  - Custom metadata attached to conversation
  - Persisted with state
  - Not sent to LLM

- **ContextPolicy**
  - Configure context window management
  - Controls token budget and truncation
  - See ContextBuilderPolicy reference

## Manager Options

Configuration options for NewConversationManager.

### WithProvider

Sets the LLM provider.

```go
func WithProvider(provider providers.Provider) ManagerOption
```

**Required.** Must be set.

**Example:**

```go
provider := providers.NewOpenAIProvider("api-key", "gpt-4o-mini", false)
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)
```

### WithStateStore

Sets the state persistence store.

```go
func WithStateStore(store statestore.Store) ManagerOption
```

**Optional.** Defaults to in-memory store.

**Example:**

```go
redisStore := statestore.NewRedisStore(redisClient)
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(redisStore),
)
```

### WithToolRegistry

Sets the tool registry for function calling.

```go
func WithToolRegistry(registry *tools.Registry) ManagerOption
```

**Optional.** Only needed if pack uses tools.

**Example:**

```go
registry := tools.NewRegistry()
registry.Register("search", searchTool)
registry.Register("calculator", calcTool)

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithToolRegistry(registry),
)
```

### WithMediaStorage

Sets the media storage service for automatic externalization of large media content.

```go
func WithMediaStorage(storageService storage.MediaStorageService) ManagerOption
```

**Optional.** Enables automatic media externalization to reduce memory usage.

**Example:**

```go
import "github.com/AltairaLabs/PromptKit/runtime/storage/local"

fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "./media",
    Organization:        local.OrganizationBySession,
    EnableDeduplication: true,
})

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),
)
```

**Behavior:**

- Automatically enables `EnableMediaExternalization` in config
- Media larger than `MediaSizeThresholdKB` stored to disk
- Reduces memory footprint by 70-90% for media-heavy applications
- Transparent to application code

**See:** [How-To: Configure Media Storage](../how-to/configure-media-storage)

### WithConfig

Sets the manager configuration.

```go
func WithConfig(config ManagerConfig) ManagerOption
```

**Optional.** Provides advanced configuration.

**Example:**

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithConfig(sdk.ManagerConfig{
        MaxConcurrentExecutions: 20,
        DefaultTimeout:          45 * time.Second,
        EnableMetrics:           true,
        MediaSizeThresholdKB:    100,
        MediaDefaultPolicy:      "retain",
    }),
)
```

## Usage Examples

### Basic Usage

```go
func basicExample() error {
    // 1. Create provider
    provider := providers.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o-mini",
        false,
    )

    // 2. Create manager
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        return err
    }

    // 3. Load pack
    pack, err := manager.LoadPack("./support.pack.json")
    if err != nil {
        return err
    }

    // 4. Create conversation
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "support",
    })
    if err != nil {
        return err
    }

    // 5. Send messages
    resp, err := conv.Send(ctx, "I need help with my order")
    if err != nil {
        return err
    }

    fmt.Printf("Assistant: %s\n", resp.Content)
    fmt.Printf("Cost: $%.4f\n", resp.Cost)
    
    return nil
}
```

### With State Persistence

```go
func persistentStateExample() error {
    // Redis state store
    redisClient := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    redisStore := statestore.NewRedisStore(redisClient)

    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(redisStore),
    )
    if err != nil {
        return err
    }

    pack, _ := manager.LoadPack("./assistant.pack.json")

    // Create conversation
    conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })

    // First turn
    resp1, _ := conv.Send(ctx, "Remember: my favorite color is blue")
    fmt.Println(resp1.Content)

    // Later: retrieve conversation
    conversationID := conv.ID()
    
    // ... (time passes, app restarts, etc.) ...

    // Retrieve and continue
    retrieved, _ := manager.GetConversation(ctx, conversationID)
    resp2, _ := retrieved.Send(ctx, "What's my favorite color?")
    fmt.Println(resp2.Content) // Should say "blue"

    return nil
}
```

### With Tools

```go
func toolsExample() error {
    // Define tools
    searchTool := &tools.Tool{
        Name:        "search",
        Description: "Search the web",
        Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
            query := args["query"].(string)
            results := performSearch(query)
            return results, nil
        },
    }

    // Create registry
    registry := tools.NewRegistry()
    registry.Register("search", searchTool)

    // Create manager with tools
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithToolRegistry(registry),
    )

    pack, _ := manager.LoadPack("./assistant.pack.json")
    conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })

    // LLM can now call search tool
    resp, _ := conv.Send(ctx, "Search for PromptKit documentation")
    fmt.Println(resp.Content) // Response using search results

    return nil
}
```

### With Media Storage

```go
func mediaStorageExample() error {
    import "github.com/AltairaLabs/PromptKit/runtime/storage/local"
    
    // Create file store
    fileStore := local.NewFileStore(local.FileStoreConfig{
        BaseDir:             "./media",
        Organization:        local.OrganizationBySession,
        EnableDeduplication: true,
    })

    // Create manager with media storage
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithMediaStorage(fileStore),
        sdk.WithConfig(sdk.ManagerConfig{
            MediaSizeThresholdKB: 100, // Externalize media > 100KB
            MediaDefaultPolicy:   "retain",
        }),
    )
    if err != nil {
        return err
    }

    pack, _ := manager.LoadPack("./vision.pack.json")
    conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        SessionID:  "session-xyz",
        PromptName: "image-analyzer",
    })

    // Generate image - automatically externalized if > 100KB
    resp, _ := conv.Send(ctx, "Generate an image of a sunset")
    
    // Large images automatically stored to ./media/session-xyz/conv-.../
    // Memory footprint reduced by ~90%
    
    fmt.Println(resp.Content)
    return nil
}
```

### Full Configuration

```go
func fullConfigExample() error {
    // Provider
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)

    // State store
    redisStore := statestore.NewRedisStore(redisClient)

    // Tool registry
    registry := tools.NewRegistry()
    registry.Register("search", searchTool)
    registry.Register("calculator", calcTool)
    
    // Media storage
    fileStore := local.NewFileStore(local.FileStoreConfig{
        BaseDir:             "/var/lib/myapp/media",
        Organization:        local.OrganizationBySession,
        EnableDeduplication: true,
    })

    // Create manager with all options
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(redisStore),
        sdk.WithToolRegistry(registry),
        sdk.WithMediaStorage(fileStore),
        sdk.WithConfig(sdk.ManagerConfig{
            MaxConcurrentExecutions: 50,
            DefaultTimeout:          60 * time.Second,
            EnableMetrics:           true,
            MediaSizeThresholdKB:    100,
            MediaDefaultPolicy:      "retain",
        }),
    )
    if err != nil {
        return err
    }

    pack, _ := manager.LoadPack("./assistant.pack.json")

    conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        SessionID:  "session-abc",
        PromptName: "assistant",
        Variables: map[string]interface{}{
            "name":     "Alice",
            "role":     "premium customer",
            "language": "English",
        },
        ContextPolicy: &middleware.ContextBuilderPolicy{
            MaxInputTokens: 8000,
            Strategy:       middleware.StrategyTruncateOldest,
        },
        Metadata: map[string]interface{}{
            "session_id": "sess_abc123",
            "source":     "web_app",
        },
    })

    resp, _ := conv.Send(ctx, "Hello")
    fmt.Println(resp.Content)

    return nil
}
```

## Thread Safety

ConversationManager is thread-safe for concurrent use:

```go
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(userID string) {
        defer wg.Done()
        
        conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
            UserID:     userID,
            PromptName: "support",
        })
        
        resp, _ := conv.Send(ctx, "Hello")
        fmt.Println(resp.Content)
    }(fmt.Sprintf("user%d", i))
}
wg.Wait()
```

## Error Handling

```go
conv, err := manager.NewConversation(ctx, pack, config)
if err != nil {
    switch {
    case errors.Is(err, sdk.ErrPackNotFound):
        // Pack doesn't exist
    case errors.Is(err, sdk.ErrPromptNotFound):
        // Prompt name not in pack
    case errors.Is(err, sdk.ErrInvalidConfig):
        // Invalid configuration
    default:
        // Other error
    }
    return err
}
```

## Best Practices

**1. Reuse the manager:**

```go
// ✅ Create once, reuse
var manager *sdk.ConversationManager

func init() {
    manager, _ = sdk.NewConversationManager(...)
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
    conv, _ := manager.NewConversation(...)
    // Use conversation
}
```

**2. Reuse packs:**

```go
// ✅ Load once, share across conversations
var supportPack *sdk.Pack

func init() {
    manager, _ = sdk.NewConversationManager(...)
    supportPack, _ = manager.LoadPack("./support.pack.json")
}

func handleSupport(userID string) {
    conv, _ := manager.NewConversation(ctx, supportPack, config)
    // Use conversation
}
```

**3. Use state stores in production:**

```go
// ✅ Persistent state for production
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(redisStore), // Not in-memory
)
```

**4. Configure context policies:**

```go
// ✅ Manage token budgets
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    ContextPolicy: &middleware.ContextBuilderPolicy{
        MaxInputTokens: 8000, // Prevent excessive costs
        Strategy:       middleware.StrategyTruncateOldest,
    },
})
```

## See Also

- [Conversation Reference](conversation) - Conversation instance methods
- [Pack Reference](pack-format) - PromptPack format
- [Storage Reference](../../runtime/reference/storage) - Media storage API
- [How-To: Initialize SDK](../how-to/initialize) - Setup guide
- [How-To: Configure Media Storage](../how-to/configure-media-storage) - Media storage configuration
- [Tutorial: First Application](../tutorials/01-first-app) - Step-by-step tutorial
