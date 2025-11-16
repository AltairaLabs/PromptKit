---
layout: default
title: Assertion Examples
nav_order: 2
parent: Arena Examples
grand_parent: PromptArena
---

# Assertion Examples

Learn validation and assertion patterns for robust LLM testing.

## Examples in this Category

### [assertions-test](assertions-test/)

**Purpose**: Comprehensive demonstration of assertion types and validation patterns

**What you'll learn:**
- Content-based assertions (contains, regex, length)
- Semantic validation (similarity, relevance, sentiment)
- Structural validation (JSON, format compliance)
- Negative assertions (must not contain)
- Multi-turn validation

**Difficulty**: Intermediate  
**Estimated time**: 30 minutes

**Featured assertions:**
- String matching and pattern validation
- Semantic similarity comparison
- JSON schema validation
- Response time checking
- Multi-provider assertion strategies

### [guardrails-test](guardrails-test/)

**Purpose**: Safety and compliance validation testing

**What you'll learn:**
- Content safety validation
- Compliance checking
- PII detection and prevention
- Inappropriate content filtering
- Safety guardrail patterns

**Difficulty**: Intermediate  
**Estimated time**: 25 minutes

**Featured patterns:**
- Negative assertions for harmful content
- Regex patterns for PII detection
- Compliance rule validation
- Multi-layer safety checks

## Getting Started

### Prerequisites

```bash
# Install PromptArena
make install-arena

# Set up provider API keys (or use mock)
export OPENAI_API_KEY="your-key"
```

### Running Assertion Examples

```bash
# Navigate to an example
cd docs/arena/examples/assertions/assertions-test

# Run tests
promptarena run

# Run with detailed output
promptarena run --verbose

# Test specific scenario
promptarena run --scenario scenario-name
```

## Key Concepts

### Layered Validation

Build assertions from basic to advanced:

```yaml
expected:
  # Layer 1: Basic presence
  - type: contains
    value: "key term"
  
  # Layer 2: Semantic validation
  - type: semantic_similarity
    baseline: "expected meaning"
    threshold: 0.85
  
  # Layer 3: Format validation
  - type: json_valid
    value: true
  
  # Layer 4: Business logic
  - type: custom
    validator: business_check
```

### Negative Validation

Test what should NOT appear:

```yaml
expected:
  # Must not contain harmful content
  - type: not_contains
    value: ["offensive", "harmful", "inappropriate"]
  
  # Must not leak PII
  - type: not_regex
    value: "\\d{3}-\\d{2}-\\d{4}"  # SSN pattern
  
  # Must not be too similar to bad response
  - type: not_similar_to
    baseline: "known bad response"
    threshold: 0.7
```

### Semantic Assertions

Validate meaning, not exact words:

```yaml
expected:
  - type: semantic_similarity
    baseline: "Paris is the capital of France"
    threshold: 0.85
  
  # Passes these variations:
  # - "France's capital is Paris"
  # - "The capital of France is Paris"
  # - "Paris, the capital city of France"
```

### Structural Validation

Validate output structure:

```yaml
expected:
  - type: json_valid
    value: true
  
  - type: json_schema
    schema:
      type: object
      properties:
        name: {type: string}
        age: {type: integer}
      required: [name, age]
```

## Assertion Patterns

### The Pyramid Pattern

```yaml
expected:
  - type: not_empty          # Base: exists
  - type: contains           # Level 2: has content
  - type: semantic_similarity # Level 3: correct meaning
  - type: custom             # Level 4: business rules
```

### The Specificity Spectrum

Balance between too loose and too strict:

```yaml
# Too loose
expected:
  - type: not_empty

# Too strict
expected:
  - type: exact_match
    value: "Exact string"

# Just right
expected:
  - type: contains
    value: "key information"
  - type: semantic_similarity
    baseline: "expected concept"
    threshold: 0.85
```

### The Safety Net

Multiple checks for different failure modes:

```yaml
expected:
  # Content safety net
  - type: contains_any
    value: ["answer1", "answer2", "answer3"]
  
  # Semantic safety net
  - type: semantic_similarity
    baseline: "expected meaning"
    threshold: 0.75
  
  # Format safety net
  - type: json_valid
    value: true
```

## Best Practices

### Start Simple

```yaml
# Begin with basic validation
expected:
  - type: contains
    value: "answer"

# Add complexity as needed
expected:
  - type: contains
    value: "answer"
  - type: semantic_similarity
    baseline: "expected meaning"
    threshold: 0.85
```

### Test Your Assertions

Verify assertions work correctly:

```yaml
# Test with known good responses
good_responses:
  - "Paris is the capital"
  - "France's capital is Paris"

# Test with known bad responses
bad_responses:
  - "London is the capital"
  - "France is a country"
```

### Use Descriptive Failures

```yaml
expected:
  - type: contains
    value: "refund policy"
    failure_message: "Response must include refund policy"
  
  - type: not_contains
    value: ["inappropriate"]
    failure_message: "Inappropriate content detected"
```

## Common Pitfalls

### Over-Specification

```yaml
# ❌ Too specific
expected:
  - type: exact_match
    value: "The capital is Paris."

# ✅ Appropriately flexible
expected:
  - type: contains
    value: "Paris"
  - type: semantic_similarity
    baseline: "Paris is the capital"
    threshold: 0.85
```

### Under-Specification

```yaml
# ❌ Too loose
expected:
  - type: not_empty

# ✅ Adequately constrained
expected:
  - type: contains
    value: ["Paris", "France"]
  - type: min_length
    value: 10
```

### Brittle Assertions

```yaml
# ❌ Breaks easily
expected:
  - type: starts_with
    value: "The answer is"

# ✅ Robust
expected:
  - type: contains
    value: "answer"
```

## Troubleshooting

### Assertion Failures

```bash
# Run with verbose output
promptarena run --verbose

# Check specific scenario
promptarena run --scenario scenario-name --verbose

# Review assertion details in output
```

### False Positives

If good responses fail:

1. Loosen threshold values
2. Use semantic similarity instead of exact match
3. Add alternative acceptable values
4. Check assertion logic

### False Negatives

If bad responses pass:

1. Add more specific assertions
2. Include negative assertions
3. Add format validation
4. Implement custom validators

## Next Steps

After mastering assertions:

1. **Real-World**: Apply in [customer-support example](../real-world/customer-support/)
2. **Multimodal**: Extend to [multimodal validation](../multimodal/)
3. **Advanced**: Learn [MCP integration testing](../mcp-integration/)

## Additional Resources

- [Explanation: Validation Strategies](../../explanation/validation-strategies.md)
- [Reference: Assertions](../../reference/assertions.md)
- [Reference: Validators](../../reference/validators.md)
- [How-To: Validate Outputs](../../how-to/validate-outputs.md)
- [Tutorial: Multi-Provider Testing](../../tutorials/02-multi-provider.md)
