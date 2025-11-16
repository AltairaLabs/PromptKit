---
layout: default
title: PromptPack Format
nav_order: 5
parent: SDK Explanation
grand_parent: SDK
---

# PromptPack Format

Understanding the design and structure of PromptPack files.

## Design Rationale

### Why PromptPacks?

**Problem:** Prompts embedded in code are hard to manage:
- Requires code changes to update prompts
- Difficult to version and test prompts
- No separation of concerns
- Hard for non-developers to edit

**Solution:** PromptPacks as compiled configuration:
- Prompts live in JSON files
- Version controlled separately
- Non-developers can edit
- Type-safe loading and validation

## Basic Structure

### Minimal Pack

```json
{
  "version": "1.0",
  "prompts": {
    "assistant": {
      "name": "assistant",
      "description": "A helpful assistant",
      "system_prompt": "You are a helpful AI assistant."
    }
  }
}
```

### Complete Pack

```json
{
  "version": "1.0",
  "metadata": {
    "name": "Customer Support Bot",
    "author": "Your Company",
    "description": "AI-powered customer support",
    "created_at": "2024-01-15T10:00:00Z"
  },
  "prompts": {
    "support_agent": {
      "name": "support_agent",
      "description": "Customer support assistant",
      "system_prompt": "You are a helpful customer support agent...",
      "available_tools": ["search_kb", "create_ticket"],
      "model_config": {
        "temperature": 0.7,
        "max_tokens": 1000,
        "top_p": 0.9
      },
      "validators": ["profanity", "pii"],
      "metadata": {
        "version": "1.2",
        "last_updated": "2024-01-20"
      }
    }
  }
}
```

## Pack Components

### Version Field

```json
{
  "version": "1.0"
}
```

**Purpose:** Schema version for forward compatibility

**Values:**
- `"1.0"` - Current version
- Future versions will support migration

### Metadata Section

```json
{
  "metadata": {
    "name": "My Bot",
    "author": "Company Name",
    "description": "Bot description",
    "created_at": "2024-01-15T10:00:00Z",
    "tags": ["customer-support", "production"]
  }
}
```

**Optional fields:**
- `name` - Human-readable pack name
- `author` - Pack creator
- `description` - Pack purpose
- `created_at` - Creation timestamp
- `tags` - Categorization tags

### Prompts Section

```json
{
  "prompts": {
    "prompt_name": {
      "name": "prompt_name",
      "description": "Prompt description",
      "system_prompt": "System prompt text",
      // ... additional fields
    }
  }
}
```

**Key:** Prompt identifier (used in code)
**Value:** Prompt configuration

## Prompt Configuration

### Required Fields

```json
{
  "name": "assistant",
  "description": "Helpful assistant",
  "system_prompt": "You are a helpful assistant."
}
```

- `name` - Must match key
- `description` - Human-readable purpose
- `system_prompt` - LLM system instructions

### Model Configuration

```json
{
  "model_config": {
    "temperature": 0.7,
    "max_tokens": 1000,
    "top_p": 0.9,
    "frequency_penalty": 0.0,
    "presence_penalty": 0.0,
    "stop_sequences": ["\n\n", "---"]
  }
}
```

**Common Parameters:**

**temperature** (0.0-2.0)
- Controls randomness
- Lower = more focused
- Higher = more creative

**max_tokens** (integer)
- Maximum response length
- Provider-specific limits

**top_p** (0.0-1.0)
- Nucleus sampling
- Alternative to temperature

**frequency_penalty** (-2.0-2.0)
- Penalize frequent tokens
- Reduces repetition

**presence_penalty** (-2.0-2.0)
- Penalize present tokens
- Increases topic diversity

**stop_sequences** (array of strings)
- Stop generation at these tokens
- Useful for structured output

### Tool Configuration

```json
{
  "available_tools": ["search", "calculator", "database"]
}
```

**Purpose:** Specify which tools this prompt can use

**Behavior:**
- SDK validates tools exist in registry
- LLM can call any listed tool
- Tools not listed are unavailable

### Validators

```json
{
  "validators": ["profanity", "pii", "length"]
}
```

**Purpose:** Specify validation rules

**Built-in Validators:**
- `profanity` - Filter inappropriate content
- `pii` - Detect personal information
- `length` - Enforce length limits

**Custom Validators:**
Register in code, reference in pack

### Examples Section

```json
{
  "examples": [
    {
      "user": "Hello, how are you?",
      "assistant": "I'm doing well, thank you! How can I help you today?"
    },
    {
      "user": "What's the weather?",
      "assistant": "I'll check the weather for you."
    }
  ]
}
```

**Purpose:** Few-shot learning examples

**Use Cases:**
- Guide response style
- Show expected behavior
- Improve consistency

## Advanced Features

### Template Variables

```json
{
  "system_prompt": "You are a {{role}} who {{behavior}}. Your expertise is in {{domain}}.",
  "template_vars": {
    "role": "helpful assistant",
    "behavior": "provides accurate information",
    "domain": "customer support"
  }
}
```

**Render at Runtime:**
```go
pack, _ := manager.LoadPack("./template.pack.json")
pack.SetVar("domain", "technical support")
```

### Conditional Prompts

```json
{
  "prompts": {
    "assistant": {
      "system_prompt": "You are a helpful assistant.",
      "variants": {
        "formal": {
          "system_prompt": "You are a professional assistant. Use formal language."
        },
        "casual": {
          "system_prompt": "You are a friendly assistant. Be casual and conversational."
        }
      }
    }
  }
}
```

**Select Variant:**
```go
config := sdk.ConversationConfig{
    PromptName: "assistant",
    Variant:    "formal",
}
```

### Environment-Specific Config

```json
{
  "prompts": {
    "assistant": {
      "system_prompt": "You are helpful.",
      "model_config": {
        "temperature": 0.7
      },
      "environments": {
        "production": {
          "model_config": {
            "temperature": 0.5
          },
          "validators": ["profanity", "pii"]
        },
        "development": {
          "model_config": {
            "temperature": 1.0
          }
        }
      }
    }
  }
}
```

**Select Environment:**
```go
pack, _ := manager.LoadPack("./assistant.pack.json", sdk.WithEnvironment("production"))
```

## Pack Compilation

### Source Format (YAML)

For better readability, write in YAML:

```yaml
version: "1.0"
metadata:
  name: Customer Support Bot
  author: Your Company

prompts:
  support_agent:
    name: support_agent
    description: Customer support assistant
    system_prompt: |
      You are a helpful customer support agent.
      Always be polite and professional.
      Use the available tools to help customers.
    
    available_tools:
      - search_kb
      - create_ticket
    
    model_config:
      temperature: 0.7
      max_tokens: 1000
```

**Compile to JSON:**
```bash
packc compile assistant.yaml -o assistant.pack.json
```

### Validation

**Pack Compiler** validates:
- Schema compliance
- Required fields
- Type checking
- Cross-references

```bash
packc validate assistant.pack.json
```

## Versioning Strategy

### Pack Versioning

```json
{
  "version": "1.0",
  "metadata": {
    "pack_version": "2.1.0"
  }
}
```

**Schema version:** SDK compatibility
**Pack version:** Your pack version

### Backward Compatibility

**Supported:**
- Adding optional fields
- Adding new prompts
- Adding metadata

**Breaking:**
- Removing required fields
- Changing field types
- Removing prompts

### Migration

```json
{
  "version": "2.0",
  "migrations": {
    "from_1_0": {
      "rename_fields": {
        "model_settings": "model_config"
      },
      "add_defaults": {
        "validators": []
      }
    }
  }
}
```

## Best Practices

### 1. Descriptive Names

```json
// Good
{
  "prompts": {
    "customer_support_agent": { },
    "technical_expert": { },
    "sales_assistant": { }
  }
}

// Bad
{
  "prompts": {
    "prompt1": { },
    "p2": { },
    "assistant": { }
  }
}
```

### 2. Clear System Prompts

```json
// Good
{
  "system_prompt": "You are a customer support agent for TechCorp. Your role is to help customers troubleshoot technical issues. Always be patient and explain steps clearly. If you cannot resolve an issue, offer to escalate to a human agent."
}

// Bad
{
  "system_prompt": "Help customers."
}
```

### 3. Appropriate Temperature

```json
{
  "model_config": {
    // Factual tasks
    "temperature": 0.3,  // Code generation, math, facts
    
    // Balanced tasks
    "temperature": 0.7,  // Chat, support, general use
    
    // Creative tasks
    "temperature": 1.0   // Writing, brainstorming
  }
}
```

### 4. Token Limits

```json
{
  "model_config": {
    "max_tokens": 500   // Short responses
    "max_tokens": 1000  // Normal conversations
    "max_tokens": 2000  // Long-form content
  }
}
```

### 5. Tool Organization

```json
{
  // Group related tools
  "available_tools": [
    "search_kb",
    "search_docs",
    "create_ticket",
    "update_ticket",
    "get_ticket_status"
  ]
}
```

## Pack Organization

### Single File

Simple projects:

```
my-app/
├── main.go
└── assistant.pack.json
```

### Multiple Packs

Organized by purpose:

```
my-app/
├── main.go
└── packs/
    ├── support.pack.json
    ├── sales.pack.json
    └── technical.pack.json
```

### Environment-Specific

```
my-app/
├── main.go
└── packs/
    ├── assistant.dev.pack.json
    ├── assistant.staging.pack.json
    └── assistant.prod.pack.json
```

## Testing Packs

### Unit Testing

```go
func TestPackLoading(t *testing.T) {
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(mockProvider),
    )
    
    pack, err := manager.LoadPack("./test.pack.json")
    if err != nil {
        t.Fatalf("failed to load pack: %v", err)
    }
    
    // Verify pack structure
    if pack.Name != "test_assistant" {
        t.Error("unexpected pack name")
    }
}
```

### Integration Testing

```go
func TestPackConversation(t *testing.T) {
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(realProvider),
    )
    
    pack, _ := manager.LoadPack("./assistant.pack.json")
    conv, _ := manager.NewConversation(ctx, pack, config)
    
    response, err := conv.Send(ctx, "Hello")
    if err != nil {
        t.Fatalf("conversation failed: %v", err)
    }
    
    // Verify response
    if response.Content == "" {
        t.Error("empty response")
    }
}
```

## Common Patterns

### Multi-Persona Pack

```json
{
  "prompts": {
    "helpful_assistant": {
      "system_prompt": "You are helpful and friendly."
    },
    "expert_advisor": {
      "system_prompt": "You are a technical expert. Use precise language."
    },
    "creative_writer": {
      "system_prompt": "You are a creative writer. Be imaginative."
    }
  }
}
```

### Hierarchical Prompts

```json
{
  "prompts": {
    "base_assistant": {
      "system_prompt": "You are helpful.",
      "model_config": { "temperature": 0.7 }
    },
    "support_assistant": {
      "extends": "base_assistant",
      "system_prompt": "You are helpful customer support.",
      "available_tools": ["search_kb"]
    }
  }
}
```

## See Also

- [PackC Documentation](../../packc/)
- [API Design Philosophy](api-design.md)
- [SDK Architecture](architecture.md)
