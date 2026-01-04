---
title: PromptKit Examples
description: Code examples and tutorials
sidebar:
  order: 0
---


This directory contains comprehensive examples demonstrating various features and use cases of PromptKit. Each example is self-contained and includes configuration files, documentation, and any necessary code.

## Example Categories

### 🤖 Arena Testing Examples

Examples demonstrating the PromptKit Arena testing framework for automated prompt evaluation and validation.

### 🔧 Integration Examples

Examples showing how to integrate PromptKit with external systems and tools.

### 🧠 Advanced Features

Examples showcasing advanced PromptKit capabilities like human-in-the-loop workflows, state management, and custom middleware.

---

## Available Examples

### 1. **assertions-test**

**Category**: Arena Testing
**Purpose**: Demonstrates how to write and use assertions for automated prompt testing

- Arena configuration for assertion-based testing
- Multiple assertion types and validation patterns
- Provider configurations for different LLM services

### 2. **context-management**

**Category**: Integration
**Purpose**: Shows context management and conversation state handling

- Context preservation across conversation turns
- State management patterns
- Memory and context injection techniques

### 3. **customer-support**

**Category**: Arena Testing
**Purpose**: Complete customer support chatbot example with arena testing

- Customer support prompt configurations
- Multi-provider testing (OpenAI, Claude, Gemini)
- Scenario-based testing for support conversations
- Pack-based prompt organization

### 4. **customer-support-integrated**

**Category**: Integration
**Purpose**: Integrated customer support system with external tool calls

- Customer information retrieval
- Support ticket creation
- Order history access
- Subscription status checking
- Multi-persona testing

### 5. **hitl-approval** ⭐

**Category**: Advanced Features
**Purpose**: Human-in-the-loop workflow for high-value operations

- **Language**: Go
- **Features**:

  - Approval workflows for sensitive operations
  - Email notification system
  - Async processing patterns
  - Custom middleware integration

- **Files**: `main.go`, `async_email_tool.go`, `mock_provider.go`

### 6. **mcp-chatbot**

**Category**: Integration
**Purpose**: Model Context Protocol (MCP) integration for chatbots

- MCP server configuration
- Protocol-based tool integration
- Chat scenario testing

### 7. **mcp-filesystem-test** ⭐

**Category**: Integration
**Purpose**: MCP integration with filesystem operations

- **Language**: Go
- **Features**:

  - Filesystem MCP server integration
  - File operations through MCP protocol
  - Testing MCP tool functionality

- **Files**: `test_filesystem.go`

### 8. **mcp-memory-test** ⭐

**Category**: Integration
**Purpose**: MCP integration with memory/storage systems

- **Language**: Go
- **Features**:

  - Memory-based MCP server testing
  - Persistent storage through MCP
  - State preservation testing

- **Files**: `test_mcp.go`

### 9. **phase2-demo** ⭐

**Category**: Advanced Features
**Purpose**: Comprehensive demonstration of PromptKit Phase 2 capabilities

- **Language**: Go
- **Features**:

  - Advanced pipeline configurations
  - Custom middleware examples
  - Multi-provider orchestration

- **Files**: `main.go`

### 10. **statestore-example**

**Category**: Advanced Features
**Purpose**: State store configuration and usage patterns

- State persistence configuration
- Arena testing with state store
- State inspection and debugging

### 11. **variables-demo** ⭐

**Category**: Arena Testing
**Purpose**: Comprehensive guide to using variables in promptconfigs

- **Features**:

  - Defining optional variables with defaults
  - Using required variables
  - Overriding variables in arena.yaml
  - Creating multiple configs from one template
  - Variable resolution priority

- **Learn**: How to make prompts flexible and reusable with template variables
- **Includes**: Restaurant bot and product support examples with detailed testing

---

## Getting Started

### Running Arena Examples

Most examples include an `arena.yaml` file for testing with the PromptKit Arena:

```bash
# Navigate to any arena example
cd examples/customer-support

# Run arena testing
promptarena run

# Inspect configuration
promptarena config inspect

# Generate reports
promptarena render
```

### Running Go Examples

Examples with Go code can be built and executed directly:

```bash
# Navigate to a Go example
cd examples/hitl-approval

# Build the example
go build

# Run the example
./hitl-approval
```

### Understanding Configuration Files

#### File Naming Conventions

PromptKit uses typed file extensions for better IDE integration and discoverability:

| Type | File Pattern | Example |
|------|-------------|---------|
| Arena | `config.arena.yaml` | `config.arena.yaml` |
| Provider | `*.provider.yaml` | `openai-gpt4.provider.yaml` |
| Scenario | `*.scenario.yaml` | `support-ticket.scenario.yaml` |
| Tool | `*.tool.yaml` | `search.tool.yaml` |
| Persona | `*.persona.yaml` | `curious-customer.persona.yaml` |
| PromptConfig | `*.prompt.yaml` | `assistant.prompt.yaml` |

**Benefits:**

- IDE automatically detects and validates files using JSON schemas
- File purposes are self-documenting
- Easy to find all files of a specific type
- Compatible with schema stores for automatic IDE integration

#### `config.arena.yaml`

Main Arena configuration file defining:

- Prompt configurations and packs
- Provider settings (OpenAI, Claude, Gemini, etc.)
- Test scenarios and assertions
- Self-play configurations

**Schema URL:** `https://promptkit.altairalabs.ai/schemas/latest/arena.json`

#### Provider Files (`providers/*.provider.yaml`)

Individual provider configurations:

- `openai-gpt4o-mini.provider.yaml` - OpenAI GPT-4o Mini
- `claude-3-5-haiku.provider.yaml` - Anthropic Claude 3.5 Haiku
- `gemini-2-0-flash.provider.yaml` - Google Gemini 2.0 Flash

**Schema URL:** `https://promptkit.altairalabs.ai/schemas/latest/provider.json`

#### Scenario Files (`scenarios/*.scenario.yaml`)

Test scenario definitions:

- Conversation flows
- Expected behaviors
- Validation criteria
- Streaming configurations

**Schema URL:** `https://promptkit.altairalabs.ai/schemas/latest/scenario.json`

#### Prompt Files (`prompts/*.prompt.yaml`)

Prompt configurations and templates:

- System prompts
- User prompts
- Prompt packs
- Template variables

**Schema URL:** `https://promptkit.altairalabs.ai/schemas/latest/promptconfig.json`

#### Adding Schema Validation

Add `$schema` at the top of any config file for IDE support:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-arena
spec:
  # ... configuration
```

This enables:

- Autocomplete in VS Code, IntelliJ, and other IDEs
- Real-time validation and error checking
- Inline documentation on hover
- Structured editing support

## Example Patterns

### Basic Arena Testing

1. Configure providers in `providers/`
2. Define prompts in `prompts/` with variables array (required and optional)
3. Set up scenarios in `scenarios/`
4. Configure arena in `arena.yaml` and override variables as needed
5. Run tests with `promptarena run`

### Working with Variables

1. Define `variables` array in your promptconfig with type, required status, and defaults
2. Set `required: true` for values that must be provided
3. Set `required: false` with `default` value for optional variables
4. Use `{{variable_name}}` syntax in `system_template`
5. Override variables in `arena.yaml` under `prompt_configs[].vars`
6. Test different configurations with scenarios
7. See `variables-demo/` for complete examples

### Integration Development

1. Implement integration logic in Go
2. Configure MCP servers if needed
3. Set up workspace in `go.work`
4. Build and test integration
5. Document usage patterns

### Advanced Workflows

1. Design custom middleware
2. Implement HITL patterns
3. Configure state management
4. Set up async processing
5. Test with arena framework

## Best Practices

### Configuration Management

- Use environment variables for sensitive data
- Organize configs by environment (dev, staging, prod)
- Version control configuration templates
- Document configuration parameters

### Testing Strategy

- Write comprehensive arena scenarios
- Test across multiple providers
- Validate edge cases and error conditions
- Use assertions for automated validation

### Code Organization

- Follow Go module best practices
- Use clear, descriptive naming
- Include comprehensive documentation
- Implement proper error handling

## Troubleshooting

### Common Issues

#### Import Path Errors

- Ensure `go.work` includes your example module
- Verify import paths match repository structure
- Check Go version compatibility

#### Arena Configuration Errors

- Validate YAML syntax with `promptarena config inspect`
- Check file paths are relative to arena.yaml
- Verify provider configurations are accessible

#### Build Failures

- Ensure all dependencies are properly versioned
- Check for Go version compatibility
- Verify replace directives in go.mod

### Getting Help

- Check individual example README files for specific guidance
- Review Arena documentation for testing framework details
- Consult SDK documentation for integration patterns
- Check troubleshooting guides in `/docs`

---

## Contributing

When adding new examples:

1. **Create descriptive directory names**
2. **Include comprehensive README.md**
3. **Add arena.yaml for testable examples**
4. **Update this main README**
5. **Test all configurations work**
6. **Document any special requirements**

### Example Template Structure

```text
new-example/
├── README.md              # Detailed example documentation
├── arena.yaml            # Arena configuration (if applicable)
├── go.mod               # Go module (if Go code)
├── main.go              # Main implementation (if Go code)
├── prompts/             # Prompt configurations
├── providers/           # Provider configurations
├── scenarios/           # Test scenarios
└── tools/              # Tool configurations (if applicable)
```

---

*This examples collection demonstrates the full power and flexibility of PromptKit across various use cases, from simple prompt testing to complex multi-system integrations.*
