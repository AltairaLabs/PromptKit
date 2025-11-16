---
layout: docs
title: Test Scenario Format
nav_order: 6
parent: Arena Reference
grand_parent: PromptArena
---


PromptPack is an **open-source specification** for defining LLM prompts, test scenarios, and configurations in a portable, version-controllable format.

## Official Documentation

For complete specification documentation, please visit:

### **[PromptPack.org](https://promptpack.org)** 📘

The official PromptPack specification site includes:

- **[Specification Overview](https://promptpack.org/docs/spec/overview)** - Understanding the PromptPack format
- **[File Format & Structure](https://promptpack.org/docs/spec/structure)** - Pack JSON structure
- **[Schema Reference](https://promptpack.org/docs/spec/schema-reference)** - JSON schema validation
- **[Real-World Examples](https://promptpack.org/docs/spec/examples)** - Complete example packs
- **[Getting Started Guide](https://promptpack.org/docs/getting-started)** - Quick start instructions
- **[Version History](https://promptpack.org/docs/spec/versions)** - v1.0, v1.1, etc.

---

## PromptArena Implementation

**PromptArena** is a reference implementation and testing tool for PromptPack files.

### Supported Features

- ✅ **PromptPack v1.1** with multimodal support (images, audio, video)
- ✅ Kubernetes-style YAML resources: `Arena`, `PromptConfig`, `Scenario`, `Provider`, `Tool`, `Persona`
- ✅ Multi-provider testing: OpenAI, Anthropic, Google Gemini, Azure, Bedrock, and Mock
- ✅ MCP (Model Context Protocol) server integration
- ✅ Comprehensive assertion framework for validation
- ✅ HTML, JSON, and Markdown output formats

### Quick Start

```bash
# Run a test scenario
promptarena run examples/arena-media-test/arena.yaml

# Test across multiple providers
promptarena run arena.yaml --provider openai,anthropic --format html
```

### Quick Links

- **Schema**: [v1.1 JSON Schema](https://promptpack.org/schema/v1.1/promptpack.schema.json)
- **Local Examples**: [`examples/`](../../examples/) directory in this repository
- **Arena Guides**: [Writing Scenarios](./writing-scenarios.md) | [Assertions](./assertions.md) | [Self-Play](./selfplay.md)
- **Community**: [GitHub Discussions](https://github.com/altairalabs/promptpack-spec/discussions)

---

## PromptArena-Specific Extensions

While implementing the PromptPack specification, PromptArena adds these testing-focused features:

### 1. Arena Configuration Resource

The `Arena` resource orchestrates testing across multiple prompts, providers, and scenarios:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-test-suite
spec:
  prompt_configs:
    - id: support
      file: prompts/support-bot.yaml
  
  providers:
    - file: providers/openai-gpt4o.yaml
    - file: providers/claude-sonnet.yaml
  
  scenarios:
    - file: scenarios/test-1.yaml
  
  # MCP server integration
  mcp_servers:
    filesystem:
      command: npx
      args: ["@modelcontextprotocol/server-filesystem", "/data"]
  
  defaults:
    output:
      dir: out
      formats: ["html", "json"]
```

### 2. Enhanced Assertions

PromptArena extends standard assertions with testing-specific validators:

```yaml
assertions:
  # Content validation
  - type: content_includes
  - type: content_matches
  - type: content_length
  
  # Tool usage validation
  - type: tools_called
  - type: tools_called_with
  
  # Multimodal validation (v1.1)
  - type: image_format
  - type: image_dimensions
  - type: audio_format
  - type: audio_duration
  - type: video_resolution
  - type: video_duration
```

See the [Assertions Guide](./assertions.md) for complete documentation.

### 3. Multimodal Testing

PromptArena implements PromptPack v1.1 multimodal support with comprehensive testing capabilities:

```yaml
# In PromptConfig
spec:
  media:
    enabled: true
    supported_types: [image, audio, video]
    image:
      max_size_mb: 20
      allowed_formats: [jpeg, png, webp]
```

```yaml
# In Scenario
turns:
  - role: user
    content:
      - type: text
        text: "What's in this image?"
      - type: image
        image_url:
          url: "path/to/image.jpg"
          detail: "high"
```

See [`examples/arena-media-test/`](../../examples/arena-media-test/) for complete examples.

### 4. Mock Provider Support

Test without API costs using the Mock provider with configurable responses:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: mock-provider
spec:
  type: mock
  model: mock-model
```

Configure responses in `providers/mock-responses.yaml`. See [Mock Provider Usage](../../examples/mcp-chatbot/MOCK_PROVIDER_USAGE.md).

### 5. Self-Play Testing

Define AI personas to automatically test conversational flows:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: frustrated-customer
spec:
  system_prompt: |
    You are a frustrated customer...
  max_turns: 8
  goal: "Get reassurance about order delivery and feel heard"
```

See the [Self-Play Guide](./selfplay.md) for details.

---

## Directory Structure

Recommended project layout for PromptArena tests:

```text
my-project/
├── arena.yaml           # Main Arena configuration
├── prompts/
│   ├── support.yaml
│   └── sales.yaml
├── scenarios/
│   ├── smoke-tests/
│   └── regression/
├── providers/
│   ├── mock.yaml
│   └── openai.yaml
├── tools/
│   └── weather.yaml
└── out/                 # Generated reports (add to .gitignore)
```

---

## Version Support

| PromptPack Version | PromptArena Support | Key Features |
|-------------------|-------------------|--------------|
| v1.0 | ✅ Full | Core specification |
| v1.1 | ✅ Full | Multimodal support (images, audio, video) |
| v1alpha1 | ✅ Full | Kubernetes-style resource format |

---

## Learn More

### PromptPack Specification
- **[PromptPack.org](https://promptpack.org)** - Official specification
- **[GitHub Repository](https://github.com/altairalabs/promptpack-spec)** - Spec source and discussions

### PromptArena Guides
- **[Writing Scenarios](./writing-scenarios.md)** - Create effective test cases
- **[Assertions Reference](./assertions.md)** - Complete assertion documentation
- **[Self-Play Testing](./selfplay.md)** - AI-driven testing with personas
- **[MCP Integration](./mcp.md)** - Model Context Protocol servers

### Examples
- [`examples/customer-support/`](../../examples/customer-support/) - Basic support bot
- [`examples/arena-media-test/`](../../examples/arena-media-test/) - Multimodal testing
- [`examples/mcp-chatbot/`](../../examples/mcp-chatbot/) - MCP server integration

---

**Questions?** Visit [PromptPack.org](https://promptpack.org) or [GitHub Discussions](https://github.com/altairalabs/promptpack-spec/discussions)
