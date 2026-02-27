---
title: Validate Outputs
---
Learn how to use assertions and validators to verify LLM responses.

## Overview

PromptArena provides built-in assertions and custom validators to verify that LLM responses meet your quality requirements.

## Built-in Assertions

### Content Assertions

#### Contains

Check if response includes specific text:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: business-hours-check

spec:
  turns:
    - role: user
      content: "What are your business hours?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Monday"]
            message: "Should mention Monday"
        
        - type: content_includes
          params:
            patterns: ["9 AM"]
            message: "Should include opening time"
```

#### Regex Match

Pattern matching:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: email-validation

spec:
  turns:
    - role: user
      content: "What's the support email?"
      assertions:
        - type: content_matches
          params:
            pattern: '[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}'
            message: "Should contain valid email"
```

#### Negative Pattern Matching

Ensure specific content is absent using negative lookahead:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: product-description

spec:
  turns:
    - role: user
      content: "Describe our product"
      assertions:
        - type: content_matches
          params:
            pattern: "^(?!.*competitor).*$"
            message: "Should not mention competitors"
```

### Structural Assertions

#### JSON Structure

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: json-validation

spec:
  turns:
    - role: user
      content: "Return user data as JSON"
      assertions:
        - type: is_valid_json
          params:
            message: "Should return valid JSON"
        
        - type: json_schema
          params:
            schema:
              type: object
              required: [name, email]
              properties:
                name:
                  type: string
                email:
                  type: string
            message: "Should match user schema"
```

### Behavioral Assertions

#### Tool Calling

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: weather-tool-check

spec:
  turns:
    - role: user
      content: "What's the weather in Paris?"
      assertions:
        - type: tools_called
          params:
            tools: ["get_weather"]
            message: "Should call weather tool"
  
  # Conversation-level assertion to check tool arguments
  conversation_assertions:
    - type: tool_calls_with_args
      params:
        tool: "get_weather"
        expected_args:
          location: "Paris"
        message: "Should pass Paris as location"
```

#### Context Retention

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: context-memory

spec:
  turns:
    - role: user
      content: "My name is Alice"
    
    - role: user
      content: "What's my name?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Alice"]
            message: "Should remember user's name"
```

## Assertion Combinations

### AND Logic (All must pass)

```yaml
turns:
  - user: "Provide customer support response"
    assertions:
      - type: content_includes
        params:
          patterns: ["thank you", "help"]
          message: "Must be helpful"
      - type: llm_judge
        params:
          criteria: "Response has positive sentiment"
          judge_provider: "openai/gpt-4o-mini"
          message: "Must be positive"
      - type: content_matches
        params:
          pattern: "^.{1,500}$"
          message: "Must be under 500 characters"
    # All assertions must pass
```

### Conditional Assertions

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: order-status-conditional

spec:
  turns:
    - role: user
      content: "Check order status"
      assertions:
        # Always validate
        - type: content_includes
          params:
            patterns: ["order"]
            message: "Should mention order"
        
        # Additional checks based on order status
        - type: content_includes
          params:
            patterns: ["shipped"]
            message: "Should mention shipping if shipped"
```

## Testing Strategies

### Progressive Validation

Start with basic assertions, add complexity:

```yaml
# Level 1: Basic structure
- type: content_matches
  params:
    pattern: ".+"
    message: "Must not be empty"

# Level 2: Content presence
- type: content_includes
  params:
    patterns: ["customer service"]
    message: "Must mention customer service"

# Level 3: Quality checks
- type: llm_judge
  params:
    criteria: "Response has positive sentiment"
    judge_provider: "openai/gpt-4o-mini"
    message: "Must be positive"
- type: llm_judge
  params:
    criteria: "Response maintains professional tone"
    judge_provider: "openai/gpt-4o-mini"
    message: "Must be professional"

# Level 4: Custom business logic
- type: llm_judge
  params:
    criteria: "Response complies with brand guidelines"
    judge_provider: "openai/gpt-4o-mini"
    message: "Must meet brand compliance"

# Level 5: External evaluation service
- type: rest_eval
  params:
    url: "https://eval-service.example.com/evaluate"
    headers:
      Authorization: "Bearer ${EVAL_API_KEY}"
    criteria: "Response meets compliance requirements"
    min_score: 0.9
    message: "External compliance check"
```

### Quality Gates

Define must-pass criteria:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: critical-path-test

spec:
  task_type: critical
  
  turns:
    - role: user
      content: "Important customer query"
      assertions:
        - type: content_includes
          params:
            patterns: ["critical terms"]
            message: "Must include critical terms"
```

### Regression Testing

Track quality over time:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: baseline-quality-check

spec:
  turns:
    - role: user
      content: "Standard query"
      assertions:
        - type: llm_judge
          params:
            criteria: "Response quality is above baseline expectations"
            judge_provider: "openai/gpt-4o-mini"
            message: "Quality should be above baseline"
```

## Output Reports

View validation results:

```bash
# JSON report with detailed assertion results
promptarena run --format json

# HTML report with visual pass/fail
promptarena run --format html

# JUnit XML for CI integration
promptarena run --format junit
```

Example JSON output:

```json
{
  "test_case": "Customer Support Response",
  "turn": 1,
  "assertions": [
    {
      "type": "content_includes",
      "expected": "thank you",
      "passed": true
    },
    {
      "type": "llm_judge",
      "expected": "pass",
      "actual": "pass",
      "passed": true
    }
  ],
  "overall_pass": true
}
```

## Best Practices

### 1. Layer Assertions

```yaml
# Structure first
- type: is_valid_json
  params:
    message: "Must be valid JSON"
- type: content_matches
  params:
    pattern: ".+"
    message: "Must not be empty"

# Then content
- type: content_includes
  params:
    patterns: ["expected data"]
    message: "Must contain expected data"

# Finally quality
- type: llm_judge
  params:
    criteria: "Response follows business rules and policies"
    judge_provider: "openai/gpt-4o-mini"
    message: "Must follow business rules"
```

### 2. Balance Strictness

```yaml
# Too strict (brittle)
- type: content_matches
  params:
    pattern: "^Thank you for contacting AcmeCorp support\\.$"
    message: "Exact match required"

# Better (flexible)
- type: content_includes
  params:
    patterns: ["thank", "AcmeCorp", "support"]
    message: "Must acknowledge support contact"
- type: llm_judge
  params:
    criteria: "Response has positive sentiment"
    judge_provider: "openai/gpt-4o-mini"
    message: "Must be positive"
```

### 3. Meaningful Error Messages

```yaml
assertions:
  - type: content_includes
    params:
      patterns: ["30 days"]
      message: "Refund responses must mention 30-day policy"
```

### 4. Test Validators

```bash
# Run with verbose output to debug validators
promptarena run --verbose --scenario validator-test
```

## Next Steps

- **[Integrate CI/CD](integrate-ci-cd)** - Automate validation in pipelines
- **[Assertions Reference](../reference/assertions)** - Complete assertion catalog
- **[Validators Reference](../reference/validators)** - Validator API details

## Examples

See validation examples:
- `examples/assertions-test/` - All assertion types
- `examples/guardrails-test/` - Custom validators
- `examples/customer-support/` - Real-world validation patterns
