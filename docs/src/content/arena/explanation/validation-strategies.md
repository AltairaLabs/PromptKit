---
title: Validation Strategies
docType: explanation
order: 4
---
# Validation Strategies

Comprehensive guide to designing effective validation and assertion strategies for LLM testing.

## The Validation Challenge

LLM outputs are non-deterministic and variable. Traditional exact-match testing doesn't work:

```yaml
# ❌ This will fail - too rigid
assertions:
  - type: content_matches
    params:
      pattern: "^The capital of France is Paris\\.$"
      message: "Exact match required"

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
assertions:
  - type: content_includes
    params:
      patterns: ["Paris"]
      message: "Should mention Paris"

# ❌ Bad: Tests exact wording
assertions:
  - type: content_matches
    params:
      pattern: "^The capital is Paris$"
      message: "Exact match"
```

### 2. Layer Your Validations

Use multiple validation types from loose to strict:

```yaml
assertions:
  # Layer 1: Basic content presence
  - type: content_includes
    params:
      patterns: ["key", "terms"]
  
  # Layer 2: Structural validation
  - type: is_valid_json
    params:
      message: "Must be valid JSON"
  
  # Layer 3: Schema validation
  - type: json_schema
    params:
      schema:
        type: object
        required: ["expected_field"]
  
  # Layer 4: Pattern matching
  - type: content_matches
    params:
      pattern: "business_rule_pattern"
```

### 3. Tolerate Variation

Build assertions that accept legitimate variation:

```yaml
# ✅ Flexible
assertions:
  - type: content_matches
    params:
      pattern: "(refund|money back|return funds)"
      message: "Should mention refund option"
  
# ❌ Too rigid
assertions:
  - type: content_includes
    params:
      patterns: ["refund policy"]
      message: "Must say exactly 'refund policy'"
```

### 4. Fail Fast, Fail Clear

Design assertions that fail with helpful messages:

```yaml
assertions:
  - type: content_includes
    params:
      patterns: ["critical_info"]
      message: "Missing required policy information"
  
  - type: content_matches
    params:
      pattern: "^(?!.*(harmful|inappropriate)).*$"
      message: "Response contains inappropriate content"
```

## Validation Types

### Content-Based Validation

#### String Contains

Check for required content:

```yaml
# Single term
assertions:
  - type: content_includes
    params:
      patterns: ["Paris"]

# Multiple terms (all must be present)
assertions:
  - type: content_includes
    params:
      patterns: ["Paris", "France", "capital"]

# Any term (at least one must be present)
assertions:
  - type: content_matches
    params:
      pattern: "(Paris|France's capital|French capital)"
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
assertions:
  - type: regex
    value: "\\+?1?\\d{9,15}"

# Email address
assertions:
  - type: regex
    value: "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"

# Date format (YYYY-MM-DD)
assertions:
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

Validate response length using regex patterns:

```yaml
# Exact length (100 characters)
assertions:
  - type: content_matches
    params:
      pattern: "^.{100}$"
      message: "Response must be exactly 100 characters"

# Range (50-200 characters)
assertions:
  - type: content_matches
    params:
      pattern: "^.{50,200}$"
      message: "Response must be 50-200 characters"

# Maximum (conciseness test - up to 150 chars)
assertions:
  - type: content_matches
    params:
      pattern: "^.{1,150}$"
      message: "Response must be at most 150 characters"

# Minimum (completeness test - at least 50 chars)
assertions:
  - type: content_matches
    params:
      pattern: "^.{50,}$"
      message: "Response must be at least 50 characters"
```

**Use when:**
- Enforcing conciseness
- Ensuring completeness
- Testing summarization
- Validating character limits

### Semantic Validation

Semantic validation can be implemented using custom validators or by combining multiple content assertions:

```yaml
turns:
  - role: user
    content: "What's the capital of France?"
    assertions:
      - type: content_includes
        params:
          patterns: ["Paris"]
          message: "Should mention Paris"
      
      - type: content_matches
        params:
          pattern: "(?i)(capital|city)"
          message: "Should reference capital/city"
```

**Use when:**
- Testing paraphrased responses
- Validating key information is present
- Checking for contextually relevant terms

#### Sentiment Analysis Analysis

Sentiment and tone can be checked using pattern matching:

```yaml
turns:
  - role: user
    content: "I'm frustrated with this issue"
    assertions:
      - type: content_matches
        params:
          pattern: "(?i)(understand|help|sorry|apologize)"
          message: "Should show empathy"
      
      - type: content_includes
        params:
          patterns: ["assist", "resolve"]
          message: "Should offer assistance"
```

**Use when:**
- Testing customer support tone
- Validating empathy
- Checking brand voice
- Ensuring professional language

### Structural Validation

#### JSON Validation

Validate JSON structure:

```yaml
# Valid JSON
assertions:
  - type: is_valid_json
    params:
      message: "Response must be valid JSON"

# JSON with schema
assertions:
  - type: json_schema
    params:
      schema:
        type: object
        properties:
          name:
            type: string
          age:
            type: integer
        required: [name, age]
      message: "Response must match schema"
```

**Use when:**
- Testing structured output
- Validating API responses
- Checking data extraction

**Example:**
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: extract-user-data

spec:
  task_type: extraction
  description: "Extract User Data"
  
  turns:
    - role: user
      content: "Extract: John Doe, age 30, john@example.com"
      assertions:
        - type: is_valid_json
          params:
            message: "Should return valid JSON"
        - type: json_schema
          params:
            schema:
              type: object
              properties:
                name: {type: string}
                age: {type: integer}
                email: {type: string, format: email}
              required: [name, age, email]
            message: "Should match user schema"
```

#### List/Array Validation

Validate lists in responses:

```yaml
turns:
  - role: user
    content: "List the top items"
    assertions:
      # Check for multiple items with pattern
      - type: content_matches
        params:
          pattern: "item1.*item2.*item3"
          message: "Should contain all items"
      
      # Check for any option
      - type: content_matches
        params:
          pattern: "(option1|option2)"
          message: "Should contain at least one option"
```

**Use when:**
- Testing enumeration tasks
- Validating option lists
- Checking recommendations

#### Format Compliance

Validate specific formats using pattern matching:

```yaml
assertions:
  # Markdown (check for markdown syntax)
  - type: content_matches
    params:
      pattern: "(^#{1,6} |\*\*|\*|`|\[.*\]\(.*\))"
      message: "Response should contain markdown formatting"
  
  # HTML (check for HTML tags)
  - type: content_matches
    params:
      pattern: "<[^>]+>"
      message: "Response should contain HTML tags"
  
  # Code block (check for code fence)
  - type: content_matches
    params:
      pattern: "```python[\\s\\S]*?```"
      message: "Response should contain Python code block"
```

### Negative Validation

Test what should NOT appear using negative lookahead patterns:

```yaml
assertions:
  # Must not contain specific words (use negative lookahead)
  - type: content_matches
    params:
      pattern: "^(?!.*(inappropriate|offensive|harmful)).*$"
      message: "Response must not contain inappropriate content"
  
  # Must not match sensitive data pattern
  - type: content_matches
    params:
      pattern: "^(?!.*\\b(password|secret|api[_-]?key)\\b).*$"
      message: "Response must not contain sensitive data keywords"
  
  # For conversation-level "not contains" checks
  # Use conversation-level assertion:
  # - type: content_not_includes
  #   params:
  #     patterns: ["inappropriate", "offensive"]
```

**Use when:**
- Testing content filtering
- Preventing data leakage
- Validating safety guardrails
- Checking compliance

**Example:**
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: no-pii-leakage

spec:
  task_type: security
  description: "No PII Leakage"
  
  turns:
    - role: user
      content: "Summarize the customer record"
      assertions:
        - type: content_matches
          params:
            pattern: "^(?!.*\\d{3}-\\d{2}-\\d{4}).*$"
            message: "Should not contain SSN"
        - type: content_matches
          params:
            pattern: "^(?!.*\\d{16}).*$"
            message: "Should not contain credit card"
        - type: content_matches
          params:
            pattern: "^(?!.*(password|secret)).*$"
            message: "Should not contain sensitive keywords"
```

### Multi-Turn Validation

Validate conversation coherence:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: context-retention

spec:
  task_type: test
  description: "Context Retention"
  
    turns:
      - role: user
        content: "My name is Alice"
        assertions:
          - type: content_includes
            params:
              patterns: ["Alice"]
              message: "Should acknowledge the name"
      
      - role: user
        content: "What's my name?"
        assertions:
          - type: content_includes
            params:
              patterns: ["Alice"]
              message: "Should remember the name"
```

**Validation types:**
```yaml
assertions:
  # References earlier context
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
assertions:
  # Base: Basic presence
  - type: content_matches
    params:
      pattern: ".+"
      message: "Response must not be empty"
  
  # Level 2: Content presence
  - type: content_includes
    params:
      patterns: ["required", "terms"]
      message: "Must contain required terms"
  
  # Level 3: Structure
  - type: is_valid_json
    params:
      message: "Response must be valid JSON"
  
  # Level 4: Semantics
  - type: llm_judge
    params:
      criteria: "Response is semantically appropriate for the query"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be semantically appropriate"
  
  # Level 5: Business logic
  - type: llm_judge
    params:
      criteria: "Response follows business rules and policies"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must follow business rules"
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
assertions:
  - type: not_empty

# Too strict (might fail good responses)
assertions:

# Just right (validates meaning, allows variation)
assertions:
  - type: content_includes
    params:
      patterns: "Paris"
```

**Guidelines:**
- Start specific, loosen as needed
- Add constraints incrementally
- Test with real LLM variations
- Balance precision and recall

### The Safety Net Pattern

Multiple validations to catch different failures:

```yaml
turns:
  - role: user
    content: "Ask a question"
    assertions:
      # Content safety net
      - type: content_matches
        params:
          pattern: "(answer1|answer2|answer3)"
          message: "Should contain one of the expected answers"
      
      # Format safety net
      - type: is_valid_json
        params:
          message: "Should return valid JSON"
      
      - type: json_path
        params:
          jmespath_expression: "required_field"
          message: "Should have required field"
```

### The Progressive Validation Pattern

Validate incrementally through conversation:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: progressive-validation

spec:
  task_type: test
  description: "Progressive Validation"
  
    turns:
      # Turn 1: Establish baseline
      - role: user
        content: "Start order"
        assertions:
          - type: content_includes
            params:
              patterns: ["order", "started"]
              message: "Should indicate order started"
      
      # Turn 2: Validate state progression
      - role: user
        content: "Add item"
        assertions:
          - type: content_includes
            params:
              patterns: ["item", "added"]
              message: "Should confirm item added"
      
      # Turn 3: Validate completion
      - role: user
        content: "Checkout"
        assertions:
          - type: content_includes
            params:
              patterns: ["order", "complete", "total", "confirmation"]
              message: "Should confirm order completion"
```

## Advanced Techniques

### Custom Validators

Write custom validation logic:

```yaml
assertions:
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

### Multiple Assertions

Combine multiple checks (all must pass):

```yaml
assertions:
  # All of these assertions must pass (implicit AND)
  - type: content_includes
    params:
      patterns: ["key_term"]
      message: "Must contain key term"
  
  - type: content_matches
    params:
      pattern: "^.{50,200}$"
      message: "Must be 50-200 characters"
  
  - type: llm_judge
    params:
      criteria: "Response has a positive tone"
      judge_provider: "openai/gpt-4o-mini"
      message: "Response should be positive"

  # For OR logic, use regex alternation:
  - type: content_matches
    params:
      pattern: "(option1|option2)"
      message: "Must contain option1 OR option2"
```

### Context-Aware Validation

Validate based on context using separate scenarios:

```yaml
# Note: Arena doesn't support conditional assertions.
# Instead, create separate scenarios for different contexts:

# Scenario 1: Premium users
- name: premium_user_support
  context:
    variables:
      user_tier: "premium"
  turns:
    - role: user
      content: "I need help"
    - role: assistant
      assertions:
        - type: content_includes
          params:
            patterns: ["priority support"]
            message: "Premium users should get priority support"

# Scenario 2: Standard users
- name: standard_user_support
  context:
    variables:
      user_tier: "standard"
  turns:
    - role: user
      content: "I need help"
    - role: assistant
      assertions:
        - type: content_includes
          params:
            patterns: ["standard support"]
            message: "Standard users should get standard support"
```

### Statistical Validation

Validate across multiple runs:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: statistical-test

spec:
  task_type: test
  description: "Statistical Test"
  
    runs: 10  # Run 10 times
    # Note: Statistical validation would require running the scenario multiple times
    # and checking aggregate results. Arena doesn't have built-in statistical
    # validation, but you can run scenarios multiple times and analyze results.
```

## Best Practices

### 1. Start Simple, Add Complexity

```yaml
# Start with basic validation
assertions:
  - type: content_includes
    params:
      patterns: "answer"

# Add semantic validation
assertions:
  - type: content_includes
    params:
      patterns: "answer"

# Add format validation
assertions:
  - type: content_includes
    params:
      patterns: "answer"
  - type: is_valid_json
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
  
  assertions:
    - type: content_includes
      params:
        patterns: "Paris"
```

### 3. Use Descriptive Failure Messages

```yaml
assertions:
  - type: content_includes
    params:
      patterns: ["refund policy"]
      message: "Response must include refund policy details"
  
  - type: content_matches
    params:
      pattern: "^(?!.*(offensive|inappropriate)).*$"
      message: "Response must not contain inappropriate language"
```

### 4. Balance Precision and Recall

```yaml
# High precision (few false positives) - exact pattern
assertions:
  - type: content_matches
    params:
      pattern: "^The specific answer is: [A-Z]$"
      message: "Must match exact format"

# High recall (few false negatives) - matches any option
assertions:
  - type: content_matches
    params:
      pattern: "(answer1|answer2|answer3)"
      message: "Must contain at least one answer"

# Balanced - specific but flexible
assertions:
  - type: content_includes
    params:
      patterns: ["answer", "option"]
      message: "Must discuss answer or option"
```

### 5. Document Validation Intent

```yaml
assertions:
  # Validate core requirement
  - type: content_includes
    params:
      patterns: ["Paris"]
      message: "Must correctly identify capital"
  
  # Validate safety
  - type: content_matches
    params:
      pattern: "^(?!.*offensive).*$"
      message: "Must maintain appropriate tone"
  
  # Validate format
  - type: is_valid_json
    params:
      message: "Output must be parseable JSON"
```

## Common Pitfalls

### Over-Specification

```yaml
# ❌ Too specific
assertions:

# ✅ Appropriately flexible
assertions:
  - type: content_includes
    params:
      patterns: "Paris"
```

### Under-Specification

```yaml
# ❌ Too loose
assertions:
  - type: content_matches
    params:
      pattern: ".+"
      message: "Must not be empty"

# ✅ Adequately constrained
assertions:
  - type: content_includes
    params:
      patterns: ["Paris", "France"]
      message: "Must mention Paris and France"
  - type: content_matches
    params:
      pattern: "^.{10,}$"
      message: "Must be at least 10 characters"
```

### Brittle Assertions

```yaml
# ❌ Breaks with minor changes
assertions:
  - type: content_matches
    params:
      pattern: "^The answer is"
      message: "Must start with exact phrase"

# ✅ Robust to variation
assertions:
  - type: content_includes
    params:
      patterns: ["answer"]
      message: "Must mention answer"
```

### Missing Negative Tests

```yaml
# ✅ Test both positive and negative
assertions:
  # Must have
  - type: content_includes
    params:
      patterns: ["correct_info"]
      message: "Must contain correct information"
  
  # Must not have (use negative lookahead)
  - type: content_matches
    params:
      pattern: "^(?!.*(incorrect|harmful)).*$"
      message: "Must not contain incorrect or harmful content"
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

- **[Testing Philosophy](testing-philosophy)** - Core testing principles
- **[Scenario Design](scenario-design)** - Designing effective scenarios
- **[Provider Comparison](provider-comparison)** - Cross-provider testing
- **[Reference: Assertions](../reference/assertions)** - Complete assertion reference
- **[Reference: Validators](../reference/validators)** - Validator documentation
