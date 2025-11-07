---
layout: page
title: "PromptPack Specification"
permalink: /docs/promptarena/promptpack-spec/
parent: PromptArena
nav_order: 2
---

# PromptPack Specification

PromptPack is an **open-source specification** for defining LLM prompts, test scenarios, and configurations in a portable, version-controllable format. It's maintained and managed by the community at **[PromptPack.org](https://promptpack.org)**.

This guide covers how PromptArena implements and extends the PromptPack specification.

## Why PromptPack?

### The Problem

The LLM ecosystem has many tools, but no standard format for:
- Defining prompts
- Describing test scenarios
- Configuring providers
- Sharing configurations

Each tool has its own format, making it hard to:
- Switch tools
- Share prompts across teams
- Version control effectively
- Build integrations

### The Solution

PromptPack provides a **Kubernetes-style YAML specification** that is:

- **Portable**: Use the same format across different tools
- **Version controlled**: Plain YAML files work great with Git
- **Modular**: Separate concerns into different resource types
- **Human-readable**: Easy to read, write, and review
- **Extensible**: Add custom fields while maintaining compatibility
- **Open**: Community-driven specification

## Core Principles

### 1. Kubernetes-Style Resources

Each PromptPack file defines a resource with:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: ResourceType
metadata:
  name: resource-name
  namespace: optional-namespace
  labels:
    key: value
spec:
  # Resource-specific configuration
```

### 2. Separation of Concerns

Different aspects are separate files:

- **Prompts**: System instructions and behavior
- **Scenarios**: Test cases and conversations
- **Providers**: Model configurations
- **Tools**: Function definitions
- **Personas**: AI characters for self-play
- **Arena**: Overall test configuration

### 3. Composability

Resources reference each other:

```yaml
# arena.yaml references other files
spec:
  prompt_configs:
    - id: support
      file: prompts/support-bot.yaml
  scenarios:
    - file: scenarios/test-1.yaml
```

## Resource Types

### Arena

The main configuration that orchestrates testing.

**Purpose**: Defines what to test, how to test it, and where to output results.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-test-suite
  labels:
    environment: staging
    team: ai-engineering
spec:
  # Link prompt configurations
  prompt_configs:
    - id: assistant      # Internal ID
      file: prompts/assistant.yaml

  # Providers to test against
  providers:
    - file: providers/openai-gpt4o.yaml
    - file: providers/claude-sonnet.yaml

  # Test scenarios
  scenarios:
    - file: scenarios/basic-qa.yaml
    - file: scenarios/edge-cases.yaml

  # Optional: Tool definitions
  tools:
    - file: tools/weather-api.yaml
    - file: tools/database-query.yaml

  # Optional: MCP server configurations
  mcp_servers:
    filesystem:
      command: npx
      args:
        - "@modelcontextprotocol/server-filesystem"
        - "/path/to/data"
      env:
        NODE_ENV: production

  # Global defaults
  defaults:
    temperature: 0.7
    max_tokens: 1500
    seed: 42
    concurrency: 3

    # Output configuration
    output:
      dir: out
      formats: ["json", "html", "markdown"]
      html:
        file: report.html
        include_metadata: true
      json:
        file: results.json
        pretty: true
      markdown:
        file: report.md

    # Failure conditions
    fail_on:
      - assertion_failure
      - provider_error
      - timeout
      - validation_error
```

**Key Fields**:
- `prompt_configs`: Maps IDs to prompt files
- `providers`: List of providers to test
- `scenarios`: Test cases to run
- `tools`: Optional tool definitions
- `mcp_servers`: Optional MCP server configs
- `defaults`: Global settings

### PromptConfig

Defines a prompt's system instructions and behavior.

**Purpose**: Encapsulates everything about how the LLM should behave.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: customer-support
  labels:
    task: support
    version: v2.1.0
spec:
  task_type: support
  version: v2.1.0
  description: "Customer support bot for e-commerce platform"

  # The main system prompt
  system_template: |
    You are a helpful customer support agent for ShopCo.

    Your capabilities:
    - Answer product questions
    - Track orders
    - Process returns and refunds
    - Troubleshoot issues
    - Escalate to humans when needed

    Tone: Professional, empathetic, solution-focused

    Guidelines:
    - Greet warmly
    - Ask clarifying questions
    - Provide clear instructions
    - Acknowledge frustration
    - Offer alternatives
    - End with "Is there anything else I can help you with?"

  # Optional: Variables that can be substituted
  required_vars:
    - company_name
    - support_email
    - hours_of_operation

  # Optional: Validators/guardrails (runtime enforcement)
  validators:
    - type: banned_words
      params:
        words:
          - "guarantee"
          - "promise"
          - "definitely"
          - "100%"
        message: "Avoid making absolute promises"

    - type: max_length
      params:
        max_characters: 1000
        max_tokens: 250
        message: "Keep responses concise"

    - type: max_sentences
      params:
        max_sentences: 8
        message: "Maximum 8 sentences per response"

    - type: required_themes
      params:
        themes:
          - professional
          - helpful
        message: "Maintain professional and helpful tone"

  # Optional: Voice and personality
  voice_profile:
    tone: professional
    characteristics:
      - helpful
      - empathetic
      - clear
      - patient
    avoid:
      - robotic
      - dismissive
      - overly casual

  # Optional: Expected model capabilities
  model_requirements:
    min_context_window: 8000
    supports_function_calling: true
    supports_streaming: true
```

**Key Fields**:
- `system_template`: The system prompt
- `required_vars`: Template variables
- `validators`: Runtime guardrails
- `voice_profile`: Tone and personality
- `task_type`: Categorization

### Scenario

Defines a test case with conversation turns and assertions.

**Purpose**: Specifies what to test and how to verify success.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: order-tracking
  labels:
    category: support
    priority: high
spec:
  task_type: support
  description: "Test order tracking conversation flow"

  # Conversation turns
  turns:
    # User message
    - role: user
      content: "I want to track my order #12345"
      assertions:
        - type: content_includes
          params:
            text: "track"
            message: "Should acknowledge tracking request"

        - type: content_matches
          params:
            pattern: "(?i)(order|#12345)"
            message: "Should reference the order number"

    # Another user message
    - role: user
      content: "It says out for delivery but I haven't received it"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(understand|help|check|contact|delivery)"
            message: "Should offer assistance"

    # Optional: Explicit assistant message (for multi-turn context)
    - role: assistant
      content: "I understand your concern. Let me check the delivery status for you."

    # Final user turn
    - role: user
      content: "Thanks for your help!"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(welcome|help|anything else)"
            message: "Should acknowledge thanks and offer further help"

  # Optional: Context about the scenario
  context:
    goal: "Verify order tracking conversation flow"
    user_type: "concerned customer"
    situation: "order delayed"
    timeline: "immediate"

  # Optional: Additional metadata
  context_metadata:
    domain: "e-commerce"
    role: "support agent"
    user_context: "customer waiting for order"
    session_goal: "resolve delivery concern"

  # Optional: Constraints
  constraints:
    max_turns: 10
    max_tokens_per_turn: 200
    required_themes:
      - professional
      - helpful
```

**Key Fields**:
- `turns`: Sequential conversation turns
- `role`: "user" or "assistant"
- `content`: Turn content
- `assertions`: Checks to verify behavior
- `context`: Scenario metadata

### Provider

Configures an LLM provider for testing.

**Purpose**: Defines which model to use and its parameters.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4o-mini
  labels:
    provider: openai
    tier: production
spec:
  type: openai          # Provider type: openai, anthropic, gemini, mock
  model: gpt-4o-mini    # Model name

  # Optional: Override default API endpoint
  base_url: https://api.openai.com/v1

  # Default parameters
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 500
    seed: 42            # For reproducibility

  # Optional: Include raw API responses in output
  include_raw_output: false

  # Optional: Cost overrides (defaults from provider)
  pricing:
    input_per_1k: 0.00015
    output_per_1k: 0.0006
```

**Authentication**: API keys are loaded from environment variables:
- OpenAI: `OPENAI_API_KEY`
- Anthropic: `ANTHROPIC_API_KEY`
- Google: `GOOGLE_API_KEY`

**Mock Provider**:
```yaml
spec:
  type: mock
  model: mock-model
  defaults:
    temperature: 0.7
    max_tokens: 500
```

### Tool

Defines a function/tool that the LLM can call.

**Purpose**: Specify external capabilities available to the LLM.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get-weather
spec:
  name: get_weather
  description: "Get current weather for a location"

  # JSON Schema for input
  input_schema:
    type: object
    properties:
      location:
        type: string
        description: "City name or coordinates"
      units:
        type: string
        enum: ["celsius", "fahrenheit"]
        default: "celsius"
    required:
      - location

  # JSON Schema for output
  output_schema:
    type: object
    properties:
      temperature:
        type: number
      conditions:
        type: string
      humidity:
        type: number

  # Execution mode
  mode: live             # "mock" | "live" | "mcp"
  timeout_ms: 5000

  # For mock mode: static response
  mock_result:
    temperature: 72
    conditions: "Sunny"
    humidity: 45

  # For live mode: HTTP configuration
  http:
    url: https://api.weather.com/v1/current
    method: POST
    headers:
      Authorization: "Bearer ${WEATHER_API_KEY}"
      Content-Type: "application/json"
    timeout_ms: 5000
```

**Modes**:
- `mock`: Return static/templated data
- `live`: Make actual HTTP calls
- `mcp`: Use MCP server (auto-discovered)

### Persona (Self-Play)

Defines an AI character for self-play testing.

**Purpose**: Create AI users to automatically test your prompts.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: frustrated-customer
spec:
  name: "Frustrated Customer"
  description: "A customer who is upset about a delayed order"

  # Persona's system prompt
  system_prompt: |
    You are a frustrated customer whose order is late.

    Your situation:
    - Order #12345 was supposed to arrive yesterday
    - You need it for an important event tomorrow
    - You've been tracking it and it's still not delivered
    - You're upset but trying to be reasonable

    Your personality:
    - Initially frustrated and impatient
    - Want quick solutions
    - Will escalate if not satisfied
    - Appreciate empathy and concrete help

    Behavior:
    - Start with a complaint
    - Ask direct questions
    - If helped well, become more understanding
    - If dismissed, become more frustrated

  # Conversation parameters
  max_turns: 8
  temperature: 0.8

  # Goal for the conversation
  goal: "Get reassurance about order delivery and feel heard"

  # Success criteria
  exit_conditions:
    - type: satisfaction_expressed
      description: "Express satisfaction with support"
    - type: escalation_requested
      description: "Ask to speak to manager (failure case)"
    - type: max_turns_reached
      description: "Conversation times out"
```

## Version Control Best Practices

### Directory Structure

```
my-project/
├── .git/
├── .gitignore
├── arena.yaml
├── prompts/
│   ├── v1/
│   │   └── assistant.yaml
│   └── v2/
│       └── assistant.yaml
├── scenarios/
│   ├── smoke-tests/
│   │   ├── basic-qa.yaml
│   │   └── edge-cases.yaml
│   └── regression/
│       ├── bug-123.yaml
│       └── bug-456.yaml
├── providers/
│   ├── dev/
│   │   └── mock.yaml
│   └── prod/
│       ├── openai.yaml
│       └── claude.yaml
└── tools/
    ├── weather.yaml
    └── database.yaml
```

### Git Ignore

```
# .gitignore
out/
*.log
.env
.DS_Store
```

### Commit Messages

```bash
# Good commit messages
git commit -m "prompts: improve customer support tone"
git commit -m "scenarios: add edge case for refunds"
git commit -m "providers: add gemini-2.0-flash"
```

## Advanced Features

### Template Variables

Use variables in prompts:

```yaml
# prompt.yaml
spec:
  system_template: |
    You are a support agent for {{company_name}}.
    Contact us: {{support_email}}
    Hours: {{hours_of_operation}}

  required_vars:
    - company_name
    - support_email
    - hours_of_operation
```

Provide values in arena.yaml:

```yaml
# arena.yaml
spec:
  prompt_configs:
    - id: support
      file: prompts/support.yaml
      vars:
        company_name: "TechCo"
        support_email: "support@techco.com"
        hours_of_operation: "9am-5pm EST"
```

### Conditional Assertions

Only run assertions under certain conditions:

```yaml
turns:
  - role: user
    content: "Call the get_weather tool"
    assertions:
      - type: tools_called
        params:
          tools: ["get_weather"]
          message: "Should call weather tool"

      - type: tools_called_with
        params:
          tool: get_weather
          expected_args:
            location: "San Francisco"
          message: "Should pass location parameter"
```

### Multi-Provider Comparison

Test the same scenario across multiple providers:

```yaml
spec:
  providers:
    - file: providers/openai-gpt4o.yaml
    - file: providers/claude-sonnet.yaml
    - file: providers/gemini-flash.yaml

  # All scenarios run against all providers
  scenarios:
    - file: scenarios/test.yaml
```

Results show side-by-side comparison in HTML report.

## Specification Versions

PromptPack uses semantic versioning:

- `v1alpha1`: Early stage, may change
- `v1beta1`: Stable, few changes expected
- `v1`: Production-ready, backward compatible

PromptArena currently supports: `promptkit.altairalabs.ai/v1alpha1`

## Extending the Specification

### Custom Fields

Add custom fields under `metadata.annotations` or `spec`:

```yaml
metadata:
  name: my-prompt
  annotations:
    custom.mycompany.com/review-status: "approved"
    custom.mycompany.com/reviewer: "alice@company.com"

spec:
  # Standard fields
  task_type: support

  # Custom extension (ignored by tools that don't understand it)
  x_custom_config:
    my_field: value
```

### Custom Validators

Create custom validator types:

```yaml
validators:
  - type: custom_sentiment
    params:
      min_sentiment: 0.5
      max_sentiment: 1.0
      message: "Response should be positive"
```

## Learn More

- **Official Spec**: [PromptPack.org](https://promptpack.org)
- **Examples**: See `examples/` in the repository
- **Schema Definitions**: See `runtime/` for Go types

## Next Steps

- **[Writing Scenarios](./writing-scenarios.md)** - Create effective test cases
- **[Assertions](./assertions.md)** - Verify LLM behavior
- **[Self-Play](./selfplay.md)** - AI-driven testing

---

**Questions about the spec?** Visit [PromptPack.org](https://promptpack.org) or open an issue on GitHub.
