---
layout: docs
title: Validation Strategies
nav_order: 4
parent: Arena Explanation
grand_parent: PromptArena
---

# Validation Strategies

Comprehensive guide to designing effective validation and assertion strategies for LLM testing.

## The Validation Challenge

LLM outputs are non-deterministic and variable. Traditional exact-match testing doesn't work:

```yaml
# ❌ This will fail randomly
expected:
  - type: exact_match
    value: "The capital of France is Paris."

# LLM might say:
# - "Paris is the capital of France."
# - "The capital of France is Paris, France."
# - "France's capital city is Paris."
```

**The core challenge:** Validate intent and correctness without demanding exact wording.

## Validation Principles

### 1. Test Behavior, Not Words

Focus on what the response achieves, not how it's phrased:

```yaml
# ✅ Good: Tests behavior
expected:
  - type: contains
    value: "Paris"
  - type: relevance
    threshold: 0.8

# ❌ Bad: Tests exact wording
expected:
  - type: exact_match
    value: "The capital is Paris"
```

### 2. Layer Your Validations

Use multiple validation types from loose to strict:

```yaml
expected:
  # Layer 1: Basic content presence
  - type: contains
    value: ["key", "terms"]
  
  # Layer 2: Structural validation
  - type: json_valid
    value: true
  
  # Layer 3: Semantic validation
  - type: semantic_similarity
    baseline: "Expected meaning"
    threshold: 0.85
  
  # Layer 4: Business logic
  - type: custom
    validator: validate_business_rules
```

### 3. Tolerate Variation

Build assertions that accept legitimate variation:

```yaml
# ✅ Flexible
expected:
  - type: contains_any
    value: ["refund", "money back", "return funds"]
  
# ❌ Too rigid
expected:
  - type: contains
    value: "refund policy"
```

### 4. Fail Fast, Fail Clear

Design assertions that fail with helpful messages:

```yaml
expected:
  - type: contains
    value: "critical_info"
    failure_message: "Missing required policy information"
  
  - type: not_contains
    value: ["harmful", "inappropriate"]
    failure_message: "Response contains inappropriate content"
```

## Validation Types

### Content-Based Validation

#### String Contains

Check for required content:

```yaml
# Single term
expected:
  - type: contains
    value: "Paris"

# Multiple terms (all must be present)
expected:
  - type: contains
    value: ["Paris", "France", "capital"]

# Any term (at least one must be present)
expected:
  - type: contains_any
    value: ["Paris", "France's capital", "French capital"]
```

**Use when:**
- Testing for required information
- Verifying key terms appear
- Checking compliance with instructions

**Limitations:**
- Doesn't validate meaning
- Can't detect context misuse
- No word order validation

#### Regular Expressions

Pattern matching for structured content:

```yaml
# Phone number format
expected:
  - type: regex
    value: "\\+?1?\\d{9,15}"

# Email address
expected:
  - type: regex
    value: "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"

# Date format (YYYY-MM-DD)
expected:
  - type: regex
    value: "\\d{4}-\\d{2}-\\d{2}"
```

**Use when:**
- Validating format compliance
- Extracting structured data
- Checking pattern adherence

**Best practices:**
- Keep patterns simple
- Use anchors (^, $) carefully
- Test pattern against variations

#### String Length

Validate response length:

```yaml
# Exact length
expected:
  - type: length
    value: 100

# Range
expected:
  - type: length_range
    min: 50
    max: 200

# Maximum (conciseness test)
expected:
  - type: max_length
    value: 150

# Minimum (completeness test)
expected:
  - type: min_length
    value: 50
```

**Use when:**
- Enforcing conciseness
- Ensuring completeness
- Testing summarization
- Validating character limits

### Semantic Validation

#### Semantic Similarity

Compare meaning, not words:

```yaml
expected:
  - type: semantic_similarity
    baseline: "Paris is the capital of France"
    threshold: 0.85  # 85% similar meaning
```

**How it works:**
- Converts text to embeddings
- Compares vector similarity (cosine distance)
- Returns score 0.0 (different) to 1.0 (identical)

**Use when:**
- Testing paraphrased responses
- Validating meaning retention
- Comparing across providers
- Testing translations

**Threshold guidelines:**
```yaml
thresholds:
  exact_meaning: 0.95   # Almost identical
  close_meaning: 0.85   # Same idea, different words
  similar_topic: 0.70   # Related content
  related: 0.50         # Loosely connected
```

**Example:**
```yaml
test_cases:
  - name: "Capital Query"
    turns:
      - user: "What's the capital of France?"
        expected:
          - type: semantic_similarity
            baseline: "The capital city of France is Paris"
            threshold: 0.85
        
        # These all pass:
        # - "Paris is France's capital."
        # - "France's capital city is Paris."
        # - "The capital of France is Paris."
```

#### Relevance

Check if response addresses the query:

```yaml
expected:
  - type: relevance
    query: "${turns[0].user}"
    threshold: 0.8
```

**Use when:**
- Testing response appropriateness
- Catching off-topic responses
- Validating context retention

#### Sentiment Analysis

Validate tone and sentiment:

```yaml
expected:
  - type: sentiment
    value: positive  # or negative, neutral

  - type: sentiment_score
    min: 0.6
    max: 1.0
```

**Use when:**
- Testing customer support tone
- Validating empathy
- Checking brand voice
- Detecting negativity

**Example:**
```yaml
test_cases:
  - name: "Support Response Tone"
    turns:
      - user: "I'm frustrated with this issue"
        expected:
          - type: sentiment
            value: empathetic
          - type: contains
            value: ["understand", "help", "sorry"]
```

### Structural Validation

#### JSON Validation

Validate JSON structure:

```yaml
# Valid JSON
expected:
  - type: json_valid
    value: true

# JSON with schema
expected:
  - type: json_schema
    schema:
      type: object
      properties:
        name:
          type: string
        age:
          type: integer
      required: [name, age]
```

**Use when:**
- Testing structured output
- Validating API responses
- Checking data extraction

**Example:**
```yaml
test_cases:
  - name: "Extract User Data"
    turns:
      - user: "Extract: John Doe, age 30, john@example.com"
        expected:
          - type: json_valid
            value: true
          - type: json_schema
            schema:
              type: object
              properties:
                name: {type: string}
                age: {type: integer}
                email: {type: string, format: email}
              required: [name, age, email]
```

#### List/Array Validation

Validate lists in responses:

```yaml
expected:
  # Contains all items
  - type: list_contains_all
    value: ["item1", "item2", "item3"]
  
  # Contains any item
  - type: list_contains_any
    value: ["option1", "option2"]
  
  # List length
  - type: list_length
    min: 3
    max: 5
```

**Use when:**
- Testing enumeration tasks
- Validating option lists
- Checking recommendations

#### Format Compliance

Validate specific formats:

```yaml
expected:
  # Markdown
  - type: format
    value: markdown
  
  # HTML
  - type: format
    value: html
  
  # Code block
  - type: contains_code_block
    language: python
```

### Negative Validation

Test what should NOT appear:

```yaml
expected:
  # Must not contain
  - type: not_contains
    value: ["inappropriate", "offensive", "harmful"]
  
  # Must not match pattern
  - type: not_regex
    value: "\\b(password|secret|api[_-]?key)\\b"
  
  # Must not be too similar
  - type: not_similar_to
    baseline: "Known bad response"
    threshold: 0.7
```

**Use when:**
- Testing content filtering
- Preventing data leakage
- Validating safety guardrails
- Checking compliance

**Example:**
```yaml
test_cases:
  - name: "No PII Leakage"
    turns:
      - user: "Summarize the customer record"
        expected:
          - type: not_regex
            value: "\\d{3}-\\d{2}-\\d{4}"  # SSN
          - type: not_regex
            value: "\\d{16}"  # Credit card
          - type: not_contains
            value: ["password", "secret"]
```

### Multi-Turn Validation

Validate conversation coherence:

```yaml
test_cases:
  - name: "Context Retention"
    turns:
      - user: "My name is Alice"
        expected:
          - type: acknowledges_name
            value: true
      
      - user: "What's my name?"
        expected:
          - type: contains
            value: "Alice"
          - type: references_previous_turn
            value: true
```

**Validation types:**
```yaml
expected:
  # References earlier context
  - type: references_previous_turn
    turn_index: 0
  
  # Maintains consistency
  - type: consistent_with_turn
    turn_index: 0
  
  # State progression
  - type: state_changed
    from: "initial"
    to: "confirmed"
```

## Validation Patterns

### The Pyramid Pattern

Layer validations from basic to advanced:

```yaml
expected:
  # Base: Basic presence
  - type: not_empty
    value: true
  
  # Level 2: Content presence
  - type: contains
    value: ["required", "terms"]
  
  # Level 3: Structure
  - type: json_valid
    value: true
  
  # Level 4: Semantics
  - type: semantic_similarity
    baseline: "Expected meaning"
    threshold: 0.85
  
  # Level 5: Business logic
  - type: custom
    validator: business_rules_check
```

**Benefits:**
- Fast failure on basic issues
- Detailed validation only if basics pass
- Clear failure diagnostics
- Efficient test execution

### The Specificity Spectrum

Balance between too loose and too strict:

```yaml
# Too loose (might pass bad responses)
expected:
  - type: not_empty

# Too strict (might fail good responses)
expected:
  - type: exact_match
    value: "The capital of France is Paris."

# Just right (validates meaning, allows variation)
expected:
  - type: contains
    value: "Paris"
  - type: semantic_similarity
    baseline: "Paris is the capital"
    threshold: 0.85
  - type: relevance
    query: "${user_query}"
    threshold: 0.8
```

**Guidelines:**
- Start specific, loosen as needed
- Add constraints incrementally
- Test with real LLM variations
- Balance precision and recall

### The Safety Net Pattern

Multiple validations to catch different failures:

```yaml
expected:
  # Content safety net
  - type: contains_any
    value: ["answer1", "answer2", "answer3"]
  
  # Semantic safety net
  - type: semantic_similarity
    baseline: "Expected concept"
    threshold: 0.75
  
  # Format safety net
  - type: json_valid
    value: true
  - type: json_path
    path: "$.required_field"
    exists: true
```

### The Progressive Validation Pattern

Validate incrementally through conversation:

```yaml
test_cases:
  - name: "Progressive Validation"
    turns:
      # Turn 1: Establish baseline
      - user: "Start order"
        expected:
          - type: state
            value: "order_started"
      
      # Turn 2: Validate state progression
      - user: "Add item"
        expected:
          - type: state
            value: "items_added"
          - type: references_previous
            value: true
      
      # Turn 3: Validate completion
      - user: "Checkout"
        expected:
          - type: state
            value: "order_complete"
          - type: contains
            value: ["total", "confirmation"]
```

## Advanced Techniques

### Custom Validators

Write custom validation logic:

```yaml
expected:
  - type: custom
    validator: check_business_hours
    args:
      timezone: "America/New_York"
```

**Implementation:**
```python
def check_business_hours(response: str, timezone: str) -> bool:
    # Extract time from response
    time_match = re.search(r'\d{1,2}:\d{2}', response)
    if not time_match:
        return False
    
    # Parse and validate
    time = datetime.strptime(time_match.group(), '%H:%M')
    return 9 <= time.hour < 17  # 9 AM - 5 PM
```

### Composite Assertions

Combine multiple checks:

```yaml
expected:
  - type: composite
    operator: AND
    assertions:
      - type: contains
        value: "key_term"
      - type: length_range
        min: 50
        max: 200
      - type: sentiment
        value: positive

  - type: composite
    operator: OR
    assertions:
      - type: contains
        value: "option1"
      - type: contains
        value: "option2"
```

### Contextual Validation

Validate based on context:

```yaml
expected:
  - type: contextual
    if:
      variable: "${user_tier}"
      equals: "premium"
    then:
      - type: contains
        value: "priority support"
    else:
      - type: contains
        value: "standard support"
```

### Statistical Validation

Validate across multiple runs:

```yaml
test_cases:
  - name: "Statistical Test"
    runs: 10  # Run 10 times
    expected:
      # Pass if >= 80% contain term
      - type: statistical
        assertion:
          type: contains
          value: "key_term"
        pass_rate: 0.8
```

## Best Practices

### 1. Start Simple, Add Complexity

```yaml
# Start with basic validation
expected:
  - type: contains
    value: "answer"

# Add semantic validation
expected:
  - type: contains
    value: "answer"
  - type: semantic_similarity
    baseline: "Expected meaning"
    threshold: 0.85

# Add format validation
expected:
  - type: contains
    value: "answer"
  - type: semantic_similarity
    baseline: "Expected meaning"
    threshold: 0.85
  - type: json_valid
    value: true
```

### 2. Test Your Validations

Run validations against known good/bad responses:

```yaml
validation_tests:
  good_responses:
    - "Paris is the capital of France"
    - "France's capital city is Paris"
    - "The capital of France is Paris"
  
  bad_responses:
    - "London is the capital"
    - "France is a country"
    - ""
  
  expected:
    - type: contains
      value: "Paris"
    - type: semantic_similarity
      baseline: "Paris is the capital"
      threshold: 0.85
```

### 3. Use Descriptive Failure Messages

```yaml
expected:
  - type: contains
    value: "refund policy"
    failure_message: "Response must include refund policy details"
  
  - type: not_contains
    value: ["offensive", "inappropriate"]
    failure_message: "Response contains inappropriate language"
```

### 4. Balance Precision and Recall

```yaml
# High precision (few false positives)
expected:
  - type: exact_match
    value: "Specific answer"

# High recall (few false negatives)
expected:
  - type: contains_any
    value: ["answer1", "answer2", "answer3"]

# Balanced
expected:
  - type: semantic_similarity
    baseline: "Expected answer"
    threshold: 0.85
```

### 5. Document Validation Intent

```yaml
expected:
  # Validate core requirement
  - type: contains
    value: "Paris"
    reason: "Must correctly identify capital"
  
  # Validate safety
  - type: not_contains
    value: ["offensive"]
    reason: "Must maintain appropriate tone"
  
  # Validate format
  - type: json_valid
    value: true
    reason: "Output must be parseable JSON"
```

## Common Pitfalls

### Over-Specification

```yaml
# ❌ Too specific
expected:
  - type: exact_match
    value: "The capital of France is Paris."

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
# ❌ Breaks with minor changes
expected:
  - type: starts_with
    value: "The answer is"

# ✅ Robust to variation
expected:
  - type: contains
    value: "answer"
```

### Missing Negative Tests

```yaml
# ✅ Test both positive and negative
expected:
  # Must have
  - type: contains
    value: "correct_info"
  
  # Must not have
  - type: not_contains
    value: ["incorrect", "harmful"]
```

## Validation Checklist

Before finalizing assertions, check:

- [ ] Tests core requirement (correctness)
- [ ] Allows legitimate variation (flexibility)
- [ ] Fails on actual errors (precision)
- [ ] Provides clear failure messages (debugging)
- [ ] Runs efficiently (performance)
- [ ] Works across providers (portability)
- [ ] Validates safety/compliance (security)
- [ ] Tests edge cases (robustness)

## Conclusion

Effective validation:
- Tests behavior, not exact words
- Layers multiple validation types
- Balances precision and flexibility
- Fails clearly and helpfully

PromptArena provides powerful validation tools that enable robust testing while accommodating LLM variability.

## Further Reading

- **[Testing Philosophy](testing-philosophy.md)** - Core testing principles
- **[Scenario Design](scenario-design.md)** - Designing effective scenarios
- **[Provider Comparison](provider-comparison.md)** - Cross-provider testing
- **[Reference: Assertions](../reference/assertions.md)** - Complete assertion reference
- **[Reference: Validators](../reference/validators.md)** - Validator documentation
