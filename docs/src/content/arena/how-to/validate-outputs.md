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

#### Not Containstern Matching

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
        
        - type: tools_called_with
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

## Custom Validators

Create custom validation logic for complex requirements.

### Validator File Structure

```yaml
# validators/custom-validators.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: custom-validators

spec:
  type: validator
  
  validators:
    - name: check_pii_removal
      description: "Ensures no PII in responses"
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: pii-testing-arena

spec:
  validators:
    - path: ./validators/custom-validators.yaml
  
  scenarios:
    - path: ./scenarios/pii-test.yaml

# In scenario
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: pii-test

spec:
  turns:
    - role: user
      content: "Tell me about user John Doe"
      assertions:
        - type: custom_validator
          params:
            validator: check_pii_removal
            message: "Should not contain PII"
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: fact-checker

spec:
  type: validator
  
  validators:
    - name: fact_check
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: fact-checking-test

spec:
  fixtures:
    known_facts:
      price: "$99"
      warranty: "2 years"
  
  turns:
    - role: user
      content: "What's the warranty period?"
      assertions:
        - type: custom_validator
          params:
            validator: fact_check
            message: "Facts should be accurate"
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
    assertions:
      - type: content_includes
        params:
          patterns: ["thank you", "help"]
      - type: sentiment
        value: positive
      - type: max_length
        value: 500
max_seconds: 2
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
- type: response_received
- type: not_empty

# Level 2: Content presence
- type: content_includes
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
params:
            seconds: 1
            message: "Must respond within 1 second"
        
        - type: custom_validator
          params:
            validator: safety_check
            message: "Must pass safety check"
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
            score: 0.85
            message: "Quality should be above 85%"
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
- type: content_includes
  value: "expected data"

# Finally quality
- type: custom
  validator: business_rules
```

### 2. Balance Strictness

```yaml
# Too strict (brittle)

# Better (flexible)
- type: content_includes
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
