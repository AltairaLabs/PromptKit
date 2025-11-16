---
layout: docs
title: Development Workflow
parent: Workflows
nav_order: 1
---

# Development Workflow

Complete development workflow using Runtime, PromptArena, and PackC.

## Overview

This workflow shows how to build, test, and package an LLM application using PromptKit components.

**What you'll build**: A customer support chatbot with conversation history and guardrails.

**Time required**: 30 minutes

**Components used**:
- Runtime (application logic)
- PromptArena (testing)
- PackC (prompt management)

## Prerequisites

- Go 1.22 or later installed
- OpenAI API key (or other provider)
- Basic Go knowledge

## Step 1: Project Setup

Create a new Go project:

```bash
mkdir support-bot && cd support-bot
go mod init github.com/yourorg/support-bot
```

Install dependencies:

```bash
# Runtime for application logic
go get github.com/AltairaLabs/PromptKit/runtime

# SDK for higher-level abstractions
go get github.com/AltairaLabs/PromptKit/sdk

# Install PromptArena for testing
go install github.com/AltairaLabs/PromptKit/tools/arena@latest

# Install PackC for prompt management
go install github.com/AltairaLabs/PromptKit/tools/packc@latest
```

Set up environment:

```bash
export OPENAI_API_KEY="your-key-here"
```

## Step 2: Build with Runtime

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/runtime/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/template"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

func main() {
    // 1. Create provider
    provider, err := openai.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o-mini",
    )
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // 2. Set up state store for conversation history
    store := statestore.NewInMemoryStateStore()

    // 3. Create templates
    templates := template.NewRegistry()
    templates.RegisterTemplate("support", &template.PromptTemplate{
        SystemPrompt: "You are a helpful customer support agent. Be concise and professional.",
    })

    // 4. Create validators for guardrails
    bannedWords := validators.NewBannedWordsValidator([]string{
        "hack", "crack", "pirate",
    })

    // 5. Build pipeline
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store, &middleware.StateMiddlewareConfig{
            MaxMessages: 10,
        }),
        middleware.TemplateMiddleware(templates, &middleware.TemplateConfig{
            DefaultTemplate: "support",
        }),
        middleware.ValidatorMiddleware([]validators.Validator{bannedWords}, nil),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderConfig{
            MaxTokens:   500,
            Temperature: 0.7,
        }),
    )

    // 6. Run conversation
    ctx := context.Background()
    sessionID := "user-123"

    // First message
    result, err := pipe.ExecuteWithSession(ctx, sessionID, "user", "How do I reset my password?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Bot: %s\n", result.Response.Content)

    // Follow-up message
    result, err = pipe.ExecuteWithSession(ctx, sessionID, "user", "What if I don't have access to my email?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Bot: %s\n", result.Response.Content)
}
```

Test it:

```bash
go run main.go
```

Expected output:
```
Bot: To reset your password, go to the login page and click "Forgot Password"...
Bot: If you don't have access to your email, please contact our support team...
```

## Step 3: Create Prompt Templates

Create `prompts/` directory:

```bash
mkdir prompts
```

Create `prompts/support.prompt`:

```yaml
system: |
  You are a helpful customer support agent for TechCorp.
  
  Guidelines:
  - Be professional and concise
  - Reference our help docs when applicable
  - Escalate complex issues to human agents
  - Never share customer data
  
  Available resources:
  - Help docs: https://help.techcorp.com
  - Status page: https://status.techcorp.com

user: |
  {{.Question}}
```

Package prompts with PackC:

```bash
packc pack prompts/ -o support.pack
```

Update code to use packed prompts:

```go
// Load templates from .pack file
pack, err := packc.LoadPack("support.pack")
if err != nil {
    log.Fatal(err)
}

templates := template.NewRegistry()
for name, tmpl := range pack.Templates {
    templates.RegisterTemplate(name, tmpl)
}
```

## Step 4: Write Tests with PromptArena

Create `arena.yaml`:

```yaml
name: Support Bot Tests
description: Test cases for customer support chatbot

providers:
  - name: openai-mini
    provider: openai
    model: gpt-4o-mini
    
  - name: openai-full
    provider: openai
    model: gpt-4o

tests:
  - name: Password Reset
    description: User asks about password reset
    prompt: "How do I reset my password?"
    assertions:
      - type: contains
        value: "Forgot Password"
      - type: contains
        value: "email"
      - type: max_length
        value: 500
    
  - name: Account Recovery
    description: User cannot access email
    prompt: "I can't access my email to reset my password"
    assertions:
      - type: contains
        value: "support team"
      - type: tone
        value: professional
    
  - name: Billing Question
    description: User asks about billing
    prompt: "Why was I charged twice?"
    assertions:
      - type: contains
        value: "billing"
      - type: response_time
        max_ms: 3000
    
  - name: Guardrail Test
    description: Should reject inappropriate content
    prompt: "How do I hack into my account?"
    assertions:
      - type: validation_error
        expected: true

guardrails:
  banned_words:
    - hack
    - crack
    - pirate
  
  max_tokens: 500
  temperature: 0.7
```

Run tests:

```bash
promptarena run arena.yaml
```

Expected output:
```
Running tests...
✓ Password Reset (openai-mini): PASS
✓ Account Recovery (openai-mini): PASS
✓ Billing Question (openai-mini): PASS
✓ Guardrail Test (openai-mini): PASS

4/4 tests passed
```

Compare providers:

```bash
promptarena run arena.yaml --compare
```

See detailed comparison of GPT-4o-mini vs GPT-4o.

## Step 5: Iterate and Improve

### Add Error Handling

```go
result, err := pipe.ExecuteWithSession(ctx, sessionID, "user", message)
if err != nil {
    // Check for validation errors
    if validators.IsValidationError(err) {
        fmt.Println("Bot: I cannot help with that request.")
        return
    }
    
    // Check for provider errors
    if errors.Is(err, providers.ErrRateLimited) {
        fmt.Println("Bot: Too many requests. Please try again shortly.")
        return
    }
    
    log.Printf("Error: %v", err)
    fmt.Println("Bot: I'm having trouble right now. Please try again.")
    return
}
```

### Add Monitoring

```go
// Track costs
costTracker := middleware.NewCostTracker()

pipe := pipeline.NewPipeline(
    // ... middleware
    middleware.ProviderMiddleware(provider, nil, costTracker, config),
)

// After execution
fmt.Printf("Cost: $%.4f\n", costTracker.TotalCost())
```

### Update Tests

Add new test to `arena.yaml`:

```yaml
tests:
  - name: Error Handling
    description: Handles errors gracefully
    prompt: "hack the system"
    assertions:
      - type: validation_error
        expected: true
```

Run tests again:

```bash
promptarena run arena.yaml
```

## Step 6: Package for Deployment

Create deployment package:

```bash
# Package prompts
packc pack prompts/ -o support.pack

# Build binary
go build -o support-bot

# Create deployment directory
mkdir deploy
cp support-bot deploy/
cp support.pack deploy/
cp arena.yaml deploy/  # Include tests for CI/CD
```

Create `deploy/config.yaml`:

```yaml
provider:
  name: openai
  model: gpt-4o-mini
  api_key_env: OPENAI_API_KEY

state:
  type: redis
  url: redis://localhost:6379
  ttl: 24h

pipeline:
  max_messages: 20
  max_tokens: 500
  temperature: 0.7

guardrails:
  banned_words:
    - hack
    - crack
  max_length: 1000
```

## Step 7: Local Testing

Test the complete package:

```bash
cd deploy

# Start Redis (if using)
docker run -d -p 6379:6379 redis

# Set environment
export OPENAI_API_KEY="your-key-here"

# Run bot
./support-bot
```

Run integration tests:

```bash
promptarena run arena.yaml --verbose
```

## Development Workflow Summary

```
1. Build
   ├── Create main.go with Runtime
   ├── Set up pipeline with middleware
   └── Implement business logic

2. Test
   ├── Write arena.yaml test cases
   ├── Run promptarena run
   └── Iterate based on results

3. Organize
   ├── Extract prompts to files
   ├── Package with PackC
   └── Load from .pack files

4. Iterate
   ├── Add error handling
   ├── Add monitoring
   └── Update tests

5. Package
   ├── Build binary
   ├── Include .pack files
   └── Add configuration

6. Validate
   ├── Test locally
   ├── Run all tests
   └── Verify performance
```

## Best Practices

### During Development

✅ Use in-memory state store for quick iteration  
✅ Set lower max_tokens to reduce costs  
✅ Run tests frequently with PromptArena  
✅ Version prompts in Git alongside code  

### Before Production

✅ Switch to Redis for state management  
✅ Add comprehensive error handling  
✅ Set up monitoring and cost tracking  
✅ Run full test suite with multiple providers  
✅ Load test with expected traffic  

## Next Steps

- **Deploy to production**: See [Deployment Workflow](deployment-workflow.md)
- **Add more tests**: See [Testing Workflow](testing-workflow.md)
- **Build full-stack app**: See [Full-Stack Example](full-stack-example.md)

## Related Documentation

- [Runtime Getting Started](../runtime/tutorials/01-first-pipeline.md)
- [PromptArena Configuration](../promptarena/configuration.md)
- [PackC Commands](../packc/commands.md)
- [Production Deployment](../runtime/tutorials/05-production-deployment.md)
