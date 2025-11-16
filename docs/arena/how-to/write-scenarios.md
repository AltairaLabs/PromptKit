---
layout: docs
title: Write Test Scenarios
nav_order: 2
parent: Arena How-To
grand_parent: PromptArena
---

# Write Test Scenarios

Learn how to create and structure test scenarios for LLM testing.

## Overview

Test scenarios define the conversation flows, expected behaviors, and validation criteria for your LLM applications. Each scenario is a YAML file in the PromptPack format.

## Basic Scenario Structure

Create a file `scenarios/basic-test.yaml`:

```yaml
version: "1.0"
task_type: support  # Links to prompt configuration

test_cases:
  - name: "Basic Customer Inquiry"
    tags: [customer-service, basic]
    
    # The conversation turns
    turns:
      - user: "What are your business hours?"
        expected:
          - type: contains
            value: "Monday"
          - type: response_time
            max_seconds: 3
      
      - user: "Do you offer weekend support?"
        expected:
          - type: contains
            value: ["Saturday", "Sunday"]
```

## Key Components

### Scenario Metadata

```yaml
version: "1.0"           # PromptPack version
task_type: support       # Links to prompt config
region: us              # Optional: regional variant
description: "Tests customer support responses"
```

### Test Cases

Each test case defines a complete conversation:

```yaml
test_cases:
  - name: "Unique Test Name"
    tags: [category, priority]  # For filtering
    context:                    # Optional scenario-specific context
      urgency: high
      topic: billing
    
    turns:
      # Conversation turns go here
```

### Conversation Turns

Define user messages and expected responses:

```yaml
turns:
  # Basic turn
  - user: "Hello, I need help"
  
  # Turn with assertions
  - user: "What's the refund policy?"
    expected:
      - type: contains
        value: "30 days"
      - type: sentiment
        value: positive
  
  # Turn with context
  - user: "Can I upgrade my plan?"
    context:
      current_plan: "Basic"
```

## Common Patterns

### Multi-turn Conversations

Test conversation flow and context retention:

```yaml
test_cases:
  - name: "Multi-turn Support Dialog"
    turns:
      - user: "I'm having issues with my account"
      
      - user: "It won't let me log in"
        expected:
          - type: contains
            value: ["password", "reset"]
      
      - user: "I already tried that"
        expected:
          - type: references_previous
            value: true
```

### Parameter Variations

Test different model settings:

```yaml
test_cases:
  - name: "Creative Response Test"
    parameters:
      temperature: 0.9
      max_tokens: 500
    
    turns:
      - user: "Generate a creative product description"
        expected:
          - type: min_length
            value: 100
```

### Tool/Function Calling

Test tool integration:

```yaml
test_cases:
  - name: "Weather Query with Tool"
    turns:
      - user: "What's the weather in San Francisco?"
        expected:
          - type: tool_called
            value: "get_weather"
          - type: contains
            value: ["temperature", "forecast"]
```

## Advanced Features

### Fixtures

Reuse common data across scenarios:

```yaml
fixtures:
  sample_user:
    name: "Jane Smith"
    account_id: "12345"
    plan: "Premium"

test_cases:
  - name: "Account-specific Test"
    context:
      user: ${fixtures.sample_user}
```

### Conditional Assertions

```yaml
turns:
  - user: "Check my order status"
    expected:
      - type: contains
        value: "shipped"
        condition:
          field: order_status
          equals: "shipped"
```

### Multiple Providers

Test the same scenario across different models:

```yaml
# In arena.yaml
scenarios:
  - path: ./scenarios/cross-provider.yaml
    providers: [openai-gpt4, claude-sonnet, gemini-pro]
```

## Best Practices

### 1. Use Descriptive Names

```yaml
# Good
- name: "Escalation: Frustrated customer with billing issue"

# Avoid
- name: "Test 1"
```

### 2. Tag Appropriately

```yaml
tags: [billing, high-priority, multi-turn, tool-calling]
```

Useful for selective test runs:

```bash
# Run only high-priority tests
promptarena run --scenario high-priority
```

### 3. Balance Specificity

```yaml
# Too specific (brittle)
expected:
  - type: exact_match
    value: "Your account balance is exactly $42.00"

# Better (flexible)
expected:
  - type: contains
    value: "account balance"
  - type: regex
    value: '\$\d+\.\d{2}'
```

### 4. Test Edge Cases

```yaml
test_cases:
  - name: "Empty Input"
    turns:
      - user: ""
        expected:
          - type: error_handled
            value: true
  
  - name: "Very Long Input"
    turns:
      - user: "${fixtures.long_text}"  # 10,000 chars
        expected:
          - type: response_received
            value: true
```

## File Organization

Structure your scenarios for maintainability:

```
scenarios/
├── customer-support/
│   ├── basic-inquiries.yaml
│   ├── billing-issues.yaml
│   └── escalations.yaml
├── content-generation/
│   ├── blog-posts.yaml
│   └── social-media.yaml
└── tool-integration/
    ├── weather.yaml
    └── database.yaml
```

## Examples

See complete working examples in the [examples directory](../../examples/).

## Next Steps

- **[Configure Providers](configure-providers.md)** - Set up LLM providers
- **[Validate Outputs](validate-outputs.md)** - Use assertions and validators
- **[Scenario Format Reference](../reference/scenario-format.md)** - Complete format specification

## Related Documentation

- **[Assertions Reference](../reference/assertions.md)** - All available assertion types
- **[Validators Reference](../reference/validators.md)** - Custom validation logic
- **[Tutorial: First Test](../tutorials/01-first-test.md)** - Step-by-step guide
