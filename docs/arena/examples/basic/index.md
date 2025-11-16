---
layout: default
title: Basic Examples
nav_order: 1
parent: Arena Examples
grand_parent: PromptArena
---

# Basic Arena Examples

Foundation examples for getting started with PromptArena testing.

## Examples in this Category

### [context-management](context-management/)

**Purpose**: Learn how to preserve context across conversation turns

**What you'll learn:**
- Context injection and management
- Multi-turn conversation patterns
- State preservation across turns
- Memory patterns in Arena tests

**Difficulty**: Beginner  
**Estimated time**: 15 minutes

### [mock-config-example.yaml](mock-config-example.yaml)

**Purpose**: Template for configuring mock providers

**What you'll learn:**
- Mock provider setup
- Response file configuration
- Testing without API costs
- Provider simulation patterns

**Difficulty**: Beginner  
**Estimated time**: 5 minutes

## Getting Started

### Prerequisites

```bash
# Install PromptArena
make install-arena

# Verify installation
promptarena --version
```

### Running Basic Examples

```bash
# Navigate to an example
cd docs/arena/examples/basic/context-management

# Run tests
promptarena run

# Use mock provider (no API keys needed)
promptarena run --provider mock
```

## Key Concepts

### Context Management

Context is critical for multi-turn conversations:

```yaml
# In scenarios/*.yaml
turns:
  - user: "My name is Alice"
    expected:
      - type: acknowledges
        value: true
  
  - user: "What's my name?"
    expected:
      - type: contains
        value: "Alice"
```

### Mock Providers

Test without API costs:

```yaml
# In arena.yaml
providers:
  - type: mock
    name: mock-provider
    responses_file: mock-responses.yaml
```

## Next Steps

After mastering basics:

1. **Assertions**: Learn validation patterns in [assertions examples](../assertions/)
2. **Real-World**: Apply concepts in [customer-support example](../real-world/customer-support/)
3. **Advanced**: Explore [MCP integration](../mcp-integration/)

## Additional Resources

- [Tutorial: Your First Test](../../tutorials/01-first-test.md)
- [How-To: Write Scenarios](../../how-to/write-scenarios.md)
- [How-To: Use Mock Providers](../../how-to/use-mock-providers.md)
- [Reference: Arena Configuration](../../reference/config-schema.md)
