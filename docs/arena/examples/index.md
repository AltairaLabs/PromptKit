---
layout: default
title: Arena Examples
nav_order: 5
parent: PromptArena
has_children: true
---

# PromptArena Examples

Practical, runnable examples demonstrating PromptArena features and real-world testing scenarios.

## Example Categories

### 🎯 [Basic Examples](basic/)
Foundation examples for getting started with Arena testing:
- **context-management**: Context preservation across conversation turns
- **mock-config-example.yaml**: Mock provider configuration template

### ✅ [Assertion Examples](assertions/)
Learn different validation and assertion patterns:
- **assertions-test**: Comprehensive assertion types and validation patterns
- **guardrails-test**: Safety and compliance validation testing

### 🔌 [MCP Integration](mcp-integration/)
Model Context Protocol integration examples:
- **mcp-chatbot**: MCP server integration for chatbots
- **mcp-filesystem-test**: Filesystem operations through MCP
- **mcp-memory-test**: Memory/storage systems with MCP

### 🎨 [Multimodal Examples](multimodal/)
Working with images, audio, and other media:
- **multimodal-basics**: Foundation for multimodal prompt testing
- **arena-media-test**: Media handling and validation

### 🏢 [Real-World Applications](real-world/)
Production-ready examples:
- **customer-support**: Complete customer support chatbot with testing
- **customer-support-integrated**: Integrated support system with tool calls

## Quick Start

### Running Any Example

```bash
# Navigate to an example directory
cd docs/arena/examples/basic/context-management

# Run the arena tests
promptarena run

# View configuration
promptarena config inspect

# Generate reports
promptarena render
```

### Example Structure

Most examples follow this structure:

```
example-name/
├── README.md              # Detailed documentation
├── arena.yaml            # Arena configuration
├── prompts/              # Prompt files
├── providers/            # Provider configurations
├── scenarios/            # Test scenarios
└── tools/               # Tool configs (optional)
```

## Learning Path

**New to Arena?** Follow this progression:

1. **Start**: [Basic context-management](basic/context-management/) - Learn the fundamentals
2. **Assertions**: [assertions-test](assertions/) - Master validation patterns
3. **Real-World**: [customer-support](real-world/customer-support/) - See it all together
4. **Advanced**: [MCP integration](mcp-integration/) - Integrate external tools
5. **Multimodal**: [multimodal-basics](multimodal/multimodal-basics/) - Work with images/media

## Example Features

### What You'll Learn

Each example demonstrates specific capabilities:

| Example | Providers | Multi-turn | Tools/MCP | Assertions | Complexity |
|---------|-----------|------------|-----------|------------|------------|
| context-management | ✅ | ✅ | ❌ | Basic | Beginner |
| assertions-test | ✅ | ✅ | ❌ | Advanced | Intermediate |
| guardrails-test | ✅ | ✅ | ❌ | Advanced | Intermediate |
| customer-support | ✅ | ✅ | ✅ | Advanced | Advanced |
| customer-support-integrated | ✅ | ✅ | ✅ | Advanced | Advanced |
| mcp-chatbot | ✅ | ✅ | ✅ | Intermediate | Advanced |
| mcp-filesystem-test | ✅ | ✅ | ✅ | Intermediate | Advanced |
| mcp-memory-test | ✅ | ✅ | ✅ | Intermediate | Advanced |
| multimodal-basics | ✅ | ✅ | ❌ | Intermediate | Intermediate |
| arena-media-test | ✅ | ✅ | ❌ | Advanced | Advanced |

## Prerequisites

### Required Tools

```bash
# Install PromptArena (from repository root)
cd /path/to/promptkit
make build-arena
make install-arena

# Verify installation
promptarena --version
```

### Provider Configuration

Examples use multiple LLM providers. Set up your API keys:

```bash
# OpenAI
export OPENAI_API_KEY="your-key-here"

# Anthropic
export ANTHROPIC_API_KEY="your-key-here"

# Google
export GOOGLE_API_KEY="your-key-here"
```

Most examples also support **mock providers** for testing without API costs:

```yaml
# In arena.yaml
providers:
  - type: mock
    name: mock-provider
    responses_file: mock-responses.yaml
```

## Working with Examples

### Modifying Examples

Examples are designed to be modified and extended:

1. **Copy the example** to your own directory
2. **Modify scenarios** in `scenarios/` folder
3. **Adjust providers** in `providers/` folder
4. **Update prompts** in `prompts/` folder
5. **Run tests** with `promptarena run`

### Testing Different Providers

Switch providers easily:

```bash
# Test with specific provider
promptarena run --provider openai-gpt4o-mini

# Test across all providers
promptarena run --all-providers

# Test with mock (no API costs)
promptarena run --provider mock
```

### Adding Assertions

Extend examples with additional validations:

```yaml
# In scenarios/*.yaml
expected:
  # Add your custom assertions
  - type: contains
    value: "expected content"
  - type: semantic_similarity
    baseline: "expected meaning"
    threshold: 0.85
  - type: response_time
    max_seconds: 3
```

## Best Practices

### When Using Examples

**Do:**
- Start simple and build complexity gradually
- Read the example README for context
- Test with mock providers first (free)
- Modify scenarios to match your use case
- Check example output for patterns

**Don't:**
- Run expensive providers without limits
- Skip reading the documentation
- Copy-paste without understanding
- Ignore validation failures
- Forget to set API keys

### Cost Management

Be mindful of API costs:

```bash
# Use mini/flash models for development
promptarena run --provider openai-gpt4o-mini

# Use mock for rapid iteration
promptarena run --provider mock

# Set limits in arena.yaml
limits:
  max_scenarios: 5
  max_turns: 3
```

## Troubleshooting

### Common Issues

**"Provider not found"**
```bash
# Check provider configuration
promptarena config inspect

# Verify API keys are set
echo $OPENAI_API_KEY
```

**"Scenario failed"**
```bash
# View detailed output
promptarena run --verbose

# Check assertion requirements
cat scenarios/scenario-name.yaml
```

**"MCP server error"**
```bash
# Verify MCP server is running
# Check MCP configuration in arena.yaml

# Test MCP connectivity
promptarena tools list
```

## Contributing Examples

Have a useful example? Contribute it!

### Example Checklist

- [ ] Comprehensive README.md with clear purpose
- [ ] Working arena.yaml configuration
- [ ] Mock provider support (no required API keys)
- [ ] Clear scenario names and descriptions
- [ ] Documented assertions and expectations
- [ ] .env.example with required variables
- [ ] Tested across multiple providers

### Submission Process

1. Create example in appropriate category
2. Test thoroughly with mock and real providers
3. Write clear documentation
4. Submit pull request with description
5. Update this index page

## Additional Resources

### Documentation
- [Getting Started Guide](../../getting-started/) - Arena installation and setup
- [How-To Guides](../how-to/) - Task-focused Arena guides
- [Tutorials](../tutorials/) - Progressive learning path
- [Reference](../reference/) - Complete API reference
- [Explanation](../explanation/) - Conceptual understanding

### Repository Examples
The [repository examples/](https://github.com/AltairaLabs/PromptKit/tree/main/examples) directory contains additional examples, including SDK and Runtime examples not covered here.

### Community
- [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions) - Ask questions
- [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues) - Report bugs
- [Contributing Guide](../../community/contributing.md) - Contribution guidelines

---

**Ready to start?** Pick an example from the categories above or follow the [learning path](#learning-path) for a structured introduction to PromptArena testing.
