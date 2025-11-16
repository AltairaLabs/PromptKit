---
title: Validate Outputs
docType: how-to
order: 5
---
# Validate Outputs

Learn how to use assertions and validators to verify LLM responses.

## Overview

PromptArena provides built-in assertions and custom validators to verify that LLM responses meet your quality requirements.

## Built-in Assertions

### Content Assertions

#### Contains

Check if response includes specific text:

```yaml
turns:
  - user: "What are your business hours?"
    expected:
      - type: contains
        value: "Monday"
      - type: contains
        value: ["9 AM", "5 PM"]  # Any of these
```

#### Regex Match

Pattern matching:

```yaml
turns:
  - user: "What's the support email?"
    expected:
      - type: regex
        value: '[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}'
```

#### Exact Match

Precise response validation:

```yaml
turns:
  - user: "What is 2+2?"
    expected:
      - type: exact_match
        value: "4"
```

#### Not Contains

Ensure specific content is absent:

```yaml
turns:
  - user: "Describe our product"
    expected:
      - type: not_contains
        value: ["competitor", "alternative"]
```

### Structural Assertions

#### Length Constraints

```yaml
turns:
  - user: "Provide a brief summary"
    expected:
      - type: max_length
        value: 200  # Max characters
      
      - type: min_length
        value: 50   # Min characters
```

#### Word Count

```yaml
turns:
  - user: "Write a tweet"
    expected:
      - type: max_words
        value: 30
```

#### JSON Structure

```yaml
turns:
  - user: "Return user data as JSON"
    expected:
      - type: valid_json
      
      - type: json_schema
        value:
          type: object
          required: [name, email]
          properties:
            name:
              type: string
            email:
              type: string
```

### Behavioral Assertions

#### Tool Calling

```yaml
turns:
  - user: "What's the weather in Paris?"
    expected:
      - type: tool_called
        value: "get_weather"
      
      - type: tool_args_match
        value:
          location: "Paris"
```

#### Response Time

```yaml
turns:
  - user: "Quick question"
    expected:
      - type: response_time
        max_seconds: 3
```

#### Context Retention

```yaml
turns:
  - user: "My name is Alice"
  
  - user: "What's my name?"
    expected:
      - type: references_previous
        value: true
      - type: contains
        value: "Alice"
```

### Quality Assertions

#### Sentiment

```yaml
turns:
  - user: "I love your product!"
    expected:
      - type: sentiment
        value: positive  # positive, negative, neutral
```

#### Tone

```yaml
turns:
  - user: "Explain this technical concept"
    expected:
      - type: tone
        value: professional  # professional, casual, formal, friendly
```

#### Language Detection

```yaml
turns:
  - user: "Respond in Spanish"
    expected:
      - type: language
        value: es
```

## Custom Validators

Create custom validation logic for complex requirements.

### Validator File Structure

```yaml
# validators/custom-validators.yaml
version: "1.0"

validators:
  - name: check_pii_removal
    description: "Ensures no PII in responses"
    type: script
    language: python
    script: |
      import re
      
      def validate(response, context):
          # Check for email addresses
          if re.search(r'\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b', response):
              return False, "Email address found in response"
          
          # Check for phone numbers
          if re.search(r'\b\d{3}[-.]?\d{3}[-.]?\d{4}\b', response):
              return False, "Phone number found in response"
          
          # Check for SSN patterns
          if re.search(r'\b\d{3}-\d{2}-\d{4}\b', response):
              return False, "SSN pattern found in response"
          
          return True, "No PII detected"
```

### Use Custom Validators

```yaml
# arena.yaml
validators:
  - path: ./validators/custom-validators.yaml

# In scenario
turns:
  - user: "Tell me about user John Doe"
    expected:
      - type: custom
        validator: check_pii_removal
```

### Advanced Validator Examples

#### Brand Consistency

```yaml
validators:
  - name: brand_check
    type: script
    language: python
    script: |
      def validate(response, context):
          brand_terms = {
              "our company": "AcmeCorp",
              "our product": "SuperWidget",
          }
          
          for wrong, correct in brand_terms.items():
              if wrong.lower() in response.lower():
                  return False, f"Use '{correct}' instead of '{wrong}'"
          
          return True, "Brand terms correct"
```

#### Factual Accuracy (with external data)

```yaml
validators:
  - name: fact_check
    type: script
    language: python
    script: |
      import json
      
      def validate(response, context):
          facts = context.get("known_facts", {})
          
          for key, value in facts.items():
              if key in response and str(value) not in response:
                  return False, f"Incorrect {key}: expected {value}"
          
          return True, "Facts verified"

# Use in scenario
test_cases:
  - name: "Fact checking test"
    context:
      known_facts:
        price: "$99"
        warranty: "2 years"
    turns:
      - user: "What's the warranty period?"
        expected:
          - type: custom
            validator: fact_check
```

#### Citation Validation

```yaml
validators:
  - name: check_citations
    type: script
    language: python
    script: |
      import re
      
      def validate(response, context):
          # Require citation format [Source: XYZ]
          citations = re.findall(r'\[Source: (.+?)\]', response)
          
          if not citations:
              return False, "No citations found"
          
          # Verify citations are in allowed sources
          allowed = context.get("allowed_sources", [])
          for cite in citations:
              if cite not in allowed:
                  return False, f"Invalid source: {cite}"
          
          return True, f"Found {len(citations)} valid citations"
```

## Assertion Combinations

### AND Logic (All must pass)

```yaml
turns:
  - user: "Provide customer support response"
    expected:
      - type: contains
        value: ["thank you", "help"]
      - type: sentiment
        value: positive
      - type: max_length
        value: 500
      - type: response_time
        max_seconds: 2
    # All assertions must pass
```

### Conditional Assertions

```yaml
turns:
  - user: "Check order status"
    expected:
      # Always validate
      - type: contains
        value: "order"
      
      # Conditional on context
      - type: contains
        value: "shipped"
        condition:
          field: order_status
          equals: "shipped"
      
      - type: contains
        value: "processing"
        condition:
          field: order_status
          equals: "pending"
```

## Testing Strategies

### Progressive Validation

Start with basic assertions, add complexity:

```yaml
# Level 1: Basic structure
- type: response_received
- type: not_empty

# Level 2: Content presence
- type: contains
  value: "customer service"

# Level 3: Quality checks
- type: sentiment
  value: positive
- type: tone
  value: professional

# Level 4: Custom business logic
- type: custom
  validator: brand_compliance
```

### Quality Gates

Define must-pass criteria:

```yaml
test_cases:
  - name: "Critical Path Test"
    quality_gates:
      pass_threshold: 100  # All assertions must pass
    
    turns:
      - user: "Important customer query"
        expected:
          - type: contains
            value: "critical terms"
          - type: response_time
            max_seconds: 1
          - type: custom
            validator: safety_check
```

### Regression Testing

Track quality over time:

```yaml
test_cases:
  - name: "Baseline Quality Check"
    baseline:
      reference_run: "2024-01-15"
      tolerance: 5  # 5% variation allowed
    
    turns:
      - user: "Standard query"
        expected:
          - type: quality_score
            min_score: 0.85
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
      "type": "contains",
      "expected": "thank you",
      "passed": true
    },
    {
      "type": "sentiment",
      "expected": "positive",
      "actual": "positive",
      "passed": true
    },
    {
      "type": "response_time",
      "max_seconds": 2,
      "actual_seconds": 1.3,
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
- type: valid_json
- type: not_empty

# Then content
- type: contains
  value: "expected data"

# Finally quality
- type: custom
  validator: business_rules
```

### 2. Balance Strictness

```yaml
# Too strict (brittle)
- type: exact_match
  value: "Thank you for contacting AcmeCorp support..."

# Better (flexible)
- type: contains
  value: ["thank", "AcmeCorp", "support"]
- type: sentiment
  value: positive
```

### 3. Meaningful Error Messages

```yaml
validators:
  - name: check_policy
    script: |
      def validate(response, context):
          if "refund" in response and "30 days" not in response:
              return False, "Refund responses must mention 30-day policy"
          return True, "Policy mentioned correctly"
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
