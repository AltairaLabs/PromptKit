# @altairalabs/promptarena

> PromptKit Arena - Multi-turn conversation simulation and testing tool for LLM applications

## Installation

### npx (No Installation Required)

```bash
npx @altairalabs/promptarena run -c ./examples/customer-support
```

### Global Installation

```bash
npm install -g @altairalabs/promptarena

# Use directly
promptarena --version
promptarena run -c ./config
```

### Project Dev Dependency

```bash
npm install --save-dev @altairalabs/promptarena

# Use via npm scripts
# Add to package.json:
{
  "scripts": {
    "test:prompts": "promptarena run -c ./tests/arena-config"
  }
}
```

## What is PromptKit Arena?

PromptKit Arena is a comprehensive testing framework for LLM-based applications. It allows you to:

- 🎯 **Test conversations** across multiple LLM providers (OpenAI, Anthropic, Google, Azure)
- 🔄 **Run multi-turn simulations** with automated agent interactions
- ✅ **Validate outputs** using assertions and quality metrics
- 📊 **Generate reports** with detailed analysis and comparisons
- 🛡️ **Test guardrails** and safety measures
- 🔧 **Validate tool usage** and function calling

## Quick Start

Get started in under 2 minutes:

```bash
# Create a new project from a template
npx @altairalabs/promptarena init my-test --quick

# Navigate to your project
cd my-test

# Set your API key (or use mock provider for testing)
export OPENAI_API_KEY=your-key-here

# Run your first test
npx @altairalabs/promptarena run

# View the HTML report
open out/report.html
```

That's it! The template includes pre-configured scenarios, assertions, and examples to get you started.

### Browse Available Templates

```bash
# List all available templates
npx @altairalabs/promptarena templates list

# Create from a specific template
npx @altairalabs/promptarena init my-project --template community/iot-maintenance-demo

# Interactive mode (choose template, provider, etc.)
npx @altairalabs/promptarena init
```

## Key Features

- 🎯 **Multi-Provider Testing** - Compare OpenAI, Anthropic, Google, and Azure side-by-side
- 🔄 **Self-Play Mode** - AI agents simulate realistic user conversations with personas
- ✅ **Turn-Level Assertions** - Validate individual responses (content, tone, length, JSON)
- 📊 **Conversation Assertions** - Check patterns across entire conversations
- 🎭 **Template & Persona System** - Dynamic prompts with variables and reusable personas
- 🛡️ **Guardrail Testing** - Ensure tools and responses follow safety constraints
- 📈 **HTML Reports** - Beautiful, detailed reports with cost tracking and metrics

## Learn More

### Assertion Types

- **Turn-Level**: `content_includes`, `content_matches`, `json_schema`, `jsonpath`, `llm_judge`, `tone`, `length`
- **Conversation-Level**: `llm_judge_conversation`, `tools_not_called_with_args`, `max_tool_calls`

See the [Assertions Guide](https://promptkit.altairalabs.ai/arena/tutorials/05-assertions/) for examples and best practices.

### Documentation

- **[Full Documentation](https://promptkit.altairalabs.ai/)** - Comprehensive guides and tutorials
- **[Configuration Reference](https://promptkit.altairalabs.ai/arena/reference/config-schema/)** - Complete schema documentation
- **[Examples](https://github.com/AltairaLabs/PromptKit/tree/main/examples)** - Working examples:
  - [Assertions Test](https://github.com/AltairaLabs/PromptKit/tree/main/examples/assertions-test) - Turn and conversation-level assertions
  - [Customer Support](https://github.com/AltairaLabs/PromptKit/tree/main/examples/customer-support) - Self-play with personas
  - [Variables Demo](https://github.com/AltairaLabs/PromptKit/tree/main/examples/variables-demo) - Template rendering
  - [LLM Judge](https://github.com/AltairaLabs/PromptKit/tree/main/examples/llm-judge) - AI-powered evaluation
- **[Multi-Turn Tutorial](https://promptkit.altairalabs.ai/arena/tutorials/03-multi-turn/)** - Self-play patterns

## License

Apache-2.0 - see [LICENSE](https://github.com/AltairaLabs/PromptKit/blob/main/LICENSE)

## Contributing

Contributions welcome! See [CONTRIBUTING.md](https://github.com/AltairaLabs/PromptKit/blob/main/CONTRIBUTING.md)

## Support

- [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues)
- [Discussions](https://github.com/AltairaLabs/PromptKit/discussions)