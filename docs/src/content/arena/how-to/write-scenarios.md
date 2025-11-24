---
title: Write Test Scenarios
docType: how-to
order: 2
---
# Write Test Scenarios

Learn how to create and structure test scenarios for LLM testing.

## Overview

Test scenarios define the conversation flows, expected behaviors, and validation criteria for your LLM applications. Each scenario is a YAML file in the PromptPack format.

## Basic Scenario Structure

Create a file `scenarios/basic-test.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-customer-inquiry
  labels:
    category: customer-service
    priority: basic

spec:
  task_type: support  # Links to prompt configuration
  description: "Tests customer support responses"
  
  # The conversation turns
  turns:
    - role: user
      content: "What are your business hours?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Monday"]
            message: "Should mention business days"
params:
            max_seconds: 3
            message: "Should respond quickly"
    
    - role: user
      content: "Do you offer weekend support?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Saturday"]
            message: "Should mention weekend availability"
```

## Key Components

### Scenario Metadata

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: support-test
  labels:
    category: support
    region: us
  annotations:
    description: "Tests customer support responses"
```

### Spec Section

The spec section contains your test configuration:

```yaml
spec:
  task_type: support
  description: "Conversation flow test"
  
  context_metadata:
    urgency: high
    topic: billing
  
  turns:
    # Conversation turns go here
```

### Conversation Turns

Define user messages and expected responses:

```yaml
spec:
  turns:
    # Basic turn
    - role: user
      content: "Hello, I need help"
    
    # Turn with assertions
    - role: user
      content: "What's the refund policy?"
      assertions:
        - type: content_includes
          params:
            patterns: ["30 days"]
            message: "Should mention refund period"
```

## Common Patterns

### Multi-turn Conversations

Test conversation flow and context retention:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: multi-turn-support-dialog

spec:
  task_type: support
  
  turns:
    - role: user
      content: "I'm having issues with my account"
    
    - role: user
      content: "It won't let me log in"
      assertions:
        - type: content_includes
          params:
            patterns: ["password"]
            message: "Should offer password reset"
    
    - role: user
      content: "I already tried that"
      assertions:
        - type: content_includes
          params:
            patterns: ["help"]
            message: "Should provide alternative help"
```

### Tool/Function Calling

Test tool integration:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: weather-query-with-tool

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "What's the weather in San Francisco?"
      assertions:
        - type: tools_called
          params:
            tools: ["get_weather"]
            message: "Should call weather tool"
        
        - type: content_includes
          params:
            patterns: ["temperature"]
            message: "Should mention temperature"
```

## Advanced Features

### Fixtures

Reuse common data across scenarios:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: account-specific-test

spec:
  task_type: support
  
  fixtures:
    sample_user:
      name: "Jane Smith"
      account_id: "12345"
      plan: "Premium"
  
  system_template: |
    You are helping user {{fixtures.sample_user.name}} 
    (Account: {{fixtures.sample_user.account_id}}).
  
  turns:
    - role: user
      content: "What's my current plan?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Premium"]
            message: "Should reference user's Premium plan"
```

### Conditional Assertions

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: order-status-check

spec:
  task_type: support
  
  turns:
    - role: user
      content: "Check my order status"
      assertions:
        - type: content_includes
          params:
            patterns: ["shipped"]
            message: "Should mention shipped status"
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: balance-check-flexible

spec:
  task_type: support
  
  turns:
    # Too specific (brittle)
    - role: user
      content: "What's my account balance?"
      assertions:
            patterns: ["Your account balance is exactly $42.00"]
            message: "Exact match - too brittle"
    
    # Better (flexible)
    - role: user
      content: "What's my account balance?"
      assertions:
        - type: content_includes
          params:
            patterns: ["account balance"]
            message: "Should mention balance"
        
        - type: content_matches
          params:
            pattern: '\$\d+\.\d{2}'
            message: "Should include dollar amount"
```

### 4. Test Edge Cases

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: edge-case-testing

spec:
  task_type: support
  
  fixtures:
    long_patterns: ["Very long text here..."]  # 10,000 chars
  
  turns:
    - role: user
      content: ""
      assertions:
        - type: content_includes
          params:
            patterns: ["help"]
            message: "Should handle empty input gracefully"
    
    - role: user
      content: "{{fixtures.long_text}}"
      assertions:
        - type: content_not_empty
          params:
            message: "Should respond to very long input"
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

- **[Configure Providers](configure-providers)** - Set up LLM providers
- **[Validate Outputs](validate-outputs)** - Use assertions and validators
- **[Scenario Format Reference](../reference/scenario-format)** - Complete format specification

## Related Documentation

- **[Assertions Reference](../reference/assertions)** - All available assertion types
- **[Validators Reference](../reference/validators)** - Custom validation logic
- **[Tutorial: First Test](../tutorials/01-first-test)** - Step-by-step guide
