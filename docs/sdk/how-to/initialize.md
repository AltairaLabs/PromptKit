---
layout: default
title: Initialize the SDK
nav_order: 1
parent: SDK How-To Guides
grand_parent: SDK
---

# How to Initialize the SDK

Learn how to set up the PromptKit SDK in your Go application.

## Prerequisites

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

## Basic Initialization

### Step 1: Import Packages

```go
import (
    "context"
    "log"
    
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)
```

### Step 2: Create Provider

Choose your LLM provider:

**OpenAI:**

```go
provider := providers.NewOpenAIProvider(
    "your-api-key",  // API key
    "gpt-4o-mini",   // Model name
    false,           // Streaming (handled by SDK)
)
```

**Anthropic (Claude):**

```go
provider := providers.NewAnthropicProvider(
    "your-api-key",
    "claude-3-5-sonnet-20241022",
    false,
)
```

**Google (Gemini):**

```go
provider := providers.NewGoogleProvider(
    "your-api-key",
    "gemini-1.5-flash",
    false,
)
```

### Step 3: Create Manager

```go
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)
if err != nil {
    log.Fatal(err)
}
```

## Configuration Options

### With State Persistence

Use Redis or Postgres for production:

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

// Redis
redisStore := statestore.NewRedisStore(redisClient)

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(redisStore),
)
```

### With Tool Support

Enable function calling:

```go
import "github.com/AltairaLabs/PromptKit/runtime/tools"

registry := tools.NewRegistry()
// Register tools later

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithToolRegistry(registry),
)
```

### With Custom Configuration

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithConfig(sdk.ManagerConfig{
        MaxConcurrentExecutions: 20,
        DefaultTimeout:          45 * time.Second,
        EnableMetrics:           true,
    }),
)
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
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
        log.Fatal(err)
    }

    // 3. Load pack
    pack, err := manager.LoadPack("./prompts.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    // 4. Create conversation
    ctx := context.Background()
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // 5. Send message
    resp, err := conv.Send(ctx, "Hello!")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Response: %s\n", resp.Content)
}
```

## Environment Variables

### Managing API Keys

Never hardcode API keys:

```go
// ✅ Good: Use environment variables
provider := providers.NewOpenAIProvider(
    os.Getenv("OPENAI_API_KEY"),
    "gpt-4o-mini",
    false,
)

// ❌ Bad: Hardcoded
provider := providers.NewOpenAIProvider(
    "sk-1234567890...",  // Never do this!
    "gpt-4o-mini",
    false,
)
```

### .env File

Create `.env` file:

```bash
OPENAI_API_KEY=sk-your-key-here
ANTHROPIC_API_KEY=sk-ant-your-key
GOOGLE_API_KEY=your-google-key
```

Load with [godotenv](https://github.com/joho/godotenv):

```go
import "github.com/joho/godotenv"

func init() {
    godotenv.Load()
}
```

## Application Structure

### Recommended Pattern

Initialize SDK once, reuse throughout application:

```go
package main

import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

var (
    manager *sdk.ConversationManager
    pack    *sdk.Pack
)

func init() {
    // Initialize provider
    provider := providers.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o-mini",
        false,
    )

    // Initialize manager (reuse across requests)
    var err error
    manager, err = sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Load pack (reuse across conversations)
    pack, err = manager.LoadPack("./prompts.pack.json")
    if err != nil {
        log.Fatal(err)
    }
}

func handleRequest(userID, message string) (string, error) {
    ctx := context.Background()
    
    // Create conversation (per user/session)
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     userID,
        PromptName: "assistant",
    })
    if err != nil {
        return "", err
    }

    // Send message
    resp, err := conv.Send(ctx, message)
    if err != nil {
        return "", err
    }

    return resp.Content, nil
}
```

## Web Server Integration

### HTTP Handler Example

```go
package main

import (
    "encoding/json"
    "net/http"
    
    "github.com/AltairaLabs/PromptKit/sdk"
)

var manager *sdk.ConversationManager
var pack *sdk.Pack

func main() {
    // Initialize SDK (once)
    initSDK()
    
    // Register handlers
    http.HandleFunc("/chat", chatHandler)
    
    http.ListenAndServe(":8080", nil)
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        UserID  string `json:"user_id"`
        Message string `json:"message"`
    }
    
    json.NewDecoder(r.Body).Decode(&req)
    
    // Create conversation
    conv, err := manager.NewConversation(r.Context(), pack, sdk.ConversationConfig{
        UserID:     req.UserID,
        PromptName: "assistant",
    })
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Send message
    resp, err := conv.Send(r.Context(), req.Message)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Return response
    json.NewEncoder(w).Encode(map[string]interface{}{
        "content": resp.Content,
        "cost":    resp.Cost,
    })
}

func initSDK() {
    provider := providers.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o-mini",
        false,
    )
    
    var err error
    manager, err = sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    pack, err = manager.LoadPack("./prompts.pack.json")
    if err != nil {
        log.Fatal(err)
    }
}
```

## Production Configuration

### Complete Production Setup

```go
func initProductionSDK() (*sdk.ConversationManager, *sdk.Pack, error) {
    // 1. Provider with retry logic
    provider := providers.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o",
        false,
    )

    // 2. Redis state store
    redisClient := redis.NewClient(&redis.Options{
        Addr:     os.Getenv("REDIS_ADDR"),
        Password: os.Getenv("REDIS_PASSWORD"),
        DB:       0,
    })
    redisStore := statestore.NewRedisStore(redisClient)

    // 3. Tool registry
    registry := tools.NewRegistry()
    registerTools(registry) // Your tool registration

    // 4. Create manager with full configuration
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(redisStore),
        sdk.WithToolRegistry(registry),
        sdk.WithConfig(sdk.ManagerConfig{
            MaxConcurrentExecutions: 100,
            DefaultTimeout:          60 * time.Second,
            EnableMetrics:           true,
        }),
    )
    if err != nil {
        return nil, nil, err
    }

    // 5. Load pack
    pack, err := manager.LoadPack("./prompts.pack.json")
    if err != nil {
        return nil, nil, err
    }

    return manager, pack, nil
}
```

## Common Mistakes

### ❌ Creating Manager Per Request

```go
// DON'T DO THIS
func handleRequest(w http.ResponseWriter, r *http.Request) {
    // Creating new manager for each request is expensive!
    manager, _ := sdk.NewConversationManager(...)
    pack, _ := manager.LoadPack(...)
    // ...
}
```

### ✅ Reuse Manager and Pack

```go
// DO THIS INSTEAD
var manager *sdk.ConversationManager
var pack *sdk.Pack

func init() {
    // Create once
    manager, _ = sdk.NewConversationManager(...)
    pack, _ = manager.LoadPack(...)
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
    // Reuse manager and pack
    conv, _ := manager.NewConversation(...)
    // ...
}
```

## Troubleshooting

### Provider Not Working

```go
// Test provider directly
resp, err := provider.SendMessage(ctx, "user", "Hello", nil)
if err != nil {
    log.Printf("Provider error: %v", err)
    // Check API key, model name, network
}
```

### Pack Not Loading

```go
pack, err := manager.LoadPack("./prompts.pack.json")
if err != nil {
    log.Printf("Pack error: %v", err)
    // Check file path, JSON format, pack validity
}

// Verify pack contents
prompts := pack.ListPrompts()
log.Printf("Available prompts: %v", prompts)
```

### State Not Persisting

```go
// Verify state store connection
store := statestore.NewRedisStore(redisClient)
err := redisClient.Ping(ctx).Err()
if err != nil {
    log.Printf("Redis connection failed: %v", err)
}
```

## Next Steps

- **[Load PromptPacks](load-packs.md)** - Load and validate packs
- **[Create Conversations](create-conversations.md)** - Start conversations
- **[Send Messages](send-messages.md)** - Send and receive messages
- **[Tutorial: First Application](../tutorials/01-first-app.md)** - Build complete app

## See Also

- [ConversationManager Reference](../reference/conversation-manager.md)
- [Provider Documentation](https://github.com/AltairaLabs/PromptKit/tree/main/runtime/providers)
- [StateStore Documentation](https://github.com/AltairaLabs/PromptKit/tree/main/runtime/statestore)
