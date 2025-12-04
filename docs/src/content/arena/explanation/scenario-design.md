---
title: Scenario Design Principles
docType: explanation
order: 2
---
# Scenario Design Principles

Understanding how to design effective, maintainable LLM test scenarios.

## What Makes a Good Test Scenario?

A well-designed test scenario is:
- **Clear**: Purpose is obvious
- **Focused**: Tests one thing well
- **Realistic**: Models actual use cases
- **Maintainable**: Easy to update
- **Robust**: Handles LLM variability

## Scenario Anatomy

### The Building Blocks

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: descriptive-name
  tags: [category, priority]

spec:
  task_type: support          # Links to prompt configuration
  
  fixtures:                   # Test-specific data
    user_tier: "premium"
  
  turns:
    - role: user
      content: "User message"
      assertions:             # Quality criteria
        - type: content_includes
          params:
            patterns: ["expected content"]
            message: "Should include expected content"
```

**Each element serves a purpose:**

- **apiVersion/kind**: Schema compatibility and resource type
- **metadata**: Identifies the scenario with name and tags
- **task_type**: Connects to prompt configuration
- **fixtures**: Reusable test data and variables
- **turns**: Conversation exchanges
- **assertions**: Quality criteria with params and messages

## Design Patterns

### Pattern 1: Single Responsibility

Each test case should validate one specific behavior:

```yaml
# ✅ Good: Tests one thing
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: greeting-response

spec:
  turns:
    - role: user
      content: "Hello"
      assertions:
        - type: content_includes
          params:
            patterns: ["hello"]
            message: "Should greet back"
        
        - type: content_matches
          params:
            pattern: "(?i)(hi|hello|welcome|nice)"
            message: "Should use friendly language"

# ❌ Avoid: Tests multiple unrelated things
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: everything-test

spec:
  turns:
    - role: user
      content: "Hello"
    - role: user
      content: "What's your refund policy?"
    - role: user
      content: "How do I contact support?"
    - role: user
      content: "What are your hours?"
```

**Why:** Single-responsibility tests are:
- Easier to debug when they fail
- More maintainable
- Better for regression testing
- Clearer in intent

### Pattern 2: Arrange-Act-Assert

Structure each turn with clear phases:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: order-issue-test

spec:
  fixtures:
    order_id: "12345"
    order_status: "shipped"
  
  turns:
    # Arrange: Set up context (via fixtures)
    # Act: LLM responds to user message
    # Assert: Verify behavior
    - role: user
      content: "I'm having an issue with my order"
      assertions:
        - type: content_includes
          params:
            patterns: ["12345"]
            message: "Should reference order ID"
```

### Pattern 3: Progressive Complexity

Start simple, build up:

```yaml
# Level 1: Basic interaction
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: simple-greeting

spec:
  turns:
    - role: user
      content: "Hi"

# Level 2: With fixtures
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: personalized-greeting

spec:
  fixtures:
    user_name: "Alice"
  
  turns:
    - role: user
      content: "Hi"
      assertions:
        - type: content_includes
          params:
            patterns: ["Alice"]
            message: "Should use user name"

# Level 3: Multi-turn
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: greeting-conversation

spec:
  turns:
    - role: user
      content: "Hi, I'm Alice"
    - role: user
      content: "What's your name?"
    - role: user
      content: "Nice to meet you"
```

### Pattern 4: Edge Case Coverage

Systematically test boundaries:

```yaml
# Happy path
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: standard-input

spec:
  turns:
    - role: user
      content: "What are your hours?"

# Empty input
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: empty-message
  tags: [edge-case]

spec:
  turns:
    - role: user
      content: ""
      assertions:
        - type: error_handled
          params:
            message: "Should handle empty input"

# Very long input
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: long-message
  tags: [edge-case]

spec:
  fixtures:
    long_patterns: ["Very long message..."]  # 10k chars
  
  turns:
    - role: user
      content: "{{fixtures.long_text}}"
      assertions:
        - type: response_received
          params:
            message: "Should handle long input"

# Special characters
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: special-characters
    tags: [edge-case]
    turns:
      - role: user
        content: "Hello <script>alert('test')</script>"
        assertions:
          - type: content_matches
            params:
              pattern: "^(?!.*<script>).*$"
              message: "Must not echo script tags"
  
  # Multiple languages
  - name: "Non-English Input"
    tags: [edge-case, i18n]
    turns:
      - role: user
        content: "¿Cuáles son sus horas?"
        assertions:
          - type: llm_judge
            params:
              criteria: "Response is in Spanish or English"
              judge_provider: "openai/gpt-4o-mini"
              message: "Must respond in appropriate language"
```

## Scenario Organization

### File Structure

Organize scenarios logically:

```
scenarios/
├── smoke/                  # Quick validation
│   └── basic.yaml
├── customer-support/       # Feature area
│   ├── greetings.yaml
│   ├── billing.yaml
│   └── technical.yaml
├── edge-cases/            # Special cases
│   ├── input-validation.yaml
│   └── error-handling.yaml
└── regression/            # Known issues
    └── bug-fixes.yaml
```

### Naming Conventions

**Files:** `feature-area.yaml`
```
customer-support.yaml
order-management.yaml
account-settings.yaml
```

**Test Cases:** `"Action/State - Variation"`
```yaml
- name: "Greeting - First Time User"
- name: "Greeting - Returning Customer"
- name: "Refund Request - Within Policy"
- name: "Refund Request - Outside Policy"
```

**Tags:** `[category, priority, type]`
```yaml
tags: [customer-service, high-priority, multi-turn]
tags: [billing, critical, regression]
tags: [onboarding, low-priority, smoke]
```

## Context Management

### When to Use Context

Context provides state for the LLM:

```yaml
# User profile context
context:
  user:
    name: "Alice"
    tier: "premium"
    account_age_days: 730
  
  current_session:
    device: "mobile"
    location: "US-CA"

turns:
  - role: user
    content: "What benefits do I have?"
    # LLM can use context in response
```

### Context vs. Conversation History

**Context**: Explicit state
```yaml
context:
  order_id: "12345"
  order_status: "shipped"
```

**History**: Implicit from previous turns
```yaml
turns:
  - role: user
    content: "My order number is 12345"
  - role: user
    content: "What's the status?"  # Refers to previous turn
```

**When to use each:**
- Use **context** for: Known state, test fixtures, environment data
- Use **history** for: Natural conversation flow, context retention testing

### Fixtures for Reusability

Define common data once:

```yaml
fixtures:
  premium_user:
    tier: "premium"
    features: ["priority_support", "advanced_analytics"]
  
  basic_user:
    tier: "basic"
    features: ["standard_support"]
  
  long_text: |
    Lorem ipsum dolor sit amet...
    (1000+ words)

turns:
  - name: "Premium User Support"
    context:
      user: ${fixtures.premium_user}
  
  - name: "Basic User Support"
    context:
      user: ${fixtures.basic_user}
```

## Assertion Design

### Layered Assertions

Apply multiple validation levels:

```yaml
assertions:
  # Layer 1: Structure
  - type: content_matches
    params:
      pattern: ".+"
      message: "Response must not be empty"
  - type: content_matches
    params:
      pattern: "^.{1,500}$"
      message: "Response must be under 500 characters"
  
  # Layer 2: Content
  - type: content_includes
    params:
      patterns: ["key information"]
      message: "Must contain key information"
  
  # Layer 3: Quality
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
  
  # Layer 4: Business Logic
  - type: llm_judge
    params:
      criteria: "Response complies with brand guidelines"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must meet brand compliance"
```

### Assertion Specificity Spectrum

Choose the right level:

```yaml
# Too loose (accepts anything)
assertions:
  - type: response_received

# Appropriate (validates behavior)
assertions:
  - type: content_includes
    params:
      patterns: ["refund", "policy"]
      message: "Should mention refund policy"

# Too strict (brittle)
assertions:
  - type: content_matches
    params:
      pattern: "^Our refund policy allows returns within 30 days\\\\.$"
      message: "Exact match - too rigid"
```

### Negative Assertions

Test what should NOT happen:

```yaml
assertions:
  # Should not mention competitors (negative lookahead)
  - type: content_matches
    params:
      pattern: "^(?!.*(CompetitorA|CompetitorB)).*$"
      message: "Must not mention competitors"
  
  # Should not be negative
  - type: llm_judge
    params:
      criteria: "Response does not have negative sentiment"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must not be negative"
  
  # Should not use inappropriate language
  - type: content_matches
    params:
      pattern: "^(?!.*(inappropriate|offensive)).*$"
      message: "Must not contain inappropriate language"
```

## Multi-Turn Design

### Conversation Flow

Design natural progressions:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: support-ticket-resolution

spec:
  task_type: test
  description: "Support Ticket Resolution"
  
    tags: [multi-turn, support]
    
    turns:
      # 1. Problem statement
      - role: user
        content: "I can't log into my account"
        assertions:
          - type: content_includes
            params:
              patterns: ["help", "account"]
      
      # 2. Information gathering
      - role: user
        content: "I get an 'invalid password' error"
        assertions:
          - type: content_includes
            params:
              patterns: ["reset", "password"]
          - type: content_includes
            params:
              patterns: ["password", "reset"]
              message: "Should reference password reset"
      
      # 3. Solution attempt
      - role: user
        content: "I tried resetting but didn't get the email"
        assertions:
          - type: content_includes
            params:
              patterns: ["spam", "check", "resend"]
      
      # 4. Resolution
      - role: user
        content: "Found it in spam, thank you!"
        assertions:
          - type: llm_judge
            params:
              criteria: "Response acknowledges resolution positively"
              judge_provider: "openai/gpt-4o-mini"
              message: "Must have positive sentiment"
```

### State Transitions

Test conversation state changes:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: booking-flow-state-machine

spec:
  task_type: test
  description: "Booking Flow State Machine"
  
    
    turns:
      # State: INIT → COLLECTING_DESTINATION
      - role: user
        content: "I want to book a flight"
        assertions:
          - type: content_includes
            params:
              patterns: "destination"
      
      # State: COLLECTING_DESTINATION → COLLECTING_DATE
      - role: user
        content: "To London"
        context:
          booking_state: "collecting_date"
        assertions:
          - type: content_includes
            params:
              patterns: ["London", "date", "when"]
      
      # State: COLLECTING_DATE → CONFIRMING
      - role: user
        content: "Next Friday"
        context:
          booking_state: "confirming"
        assertions:
          - type: content_includes
            params:
              patterns: ["confirm", "London", "Friday"]
```

### Branch Testing

Test conversation branches:

```yaml
turns:
  # Path A: Customer satisfied
turns:
      - role: user
        content: "Issue with order"
      - role: user
        content: "Order #12345"
      - role: user
        content: "That solved it, thanks!"
        assertions:
          - type: llm_judge
            params:
              criteria: "Response shows customer satisfaction"
              judge_provider: "openai/gpt-4o-mini"
              message: "Must have positive sentiment"
  
  # Path B: Customer needs escalation
turns:
      - role: user
        content: "Issue with order"
      - role: user
        content: "Order #12345"
      - role: user
        content: "That doesn't help, I need a manager"
        assertions:
          - type: content_includes
            params:
              patterns: ["manager", "supervisor", "escalate"]
```

## Performance Considerations

### Test Execution Speed

Balance coverage with speed:

```yaml
# Fast: Smoke tests (mock provider)
smoke_tests:
  runtime: < 30 seconds
  provider: mock
  scenarios: 10

# Medium: Integration tests
integration_tests:
  runtime: < 5 minutes
  provider: gpt-4o-mini
  scenarios: 50

# Slow: Comprehensive tests
comprehensive_tests:
  runtime: < 20 minutes
  provider: [gpt-4o, claude, gemini]
  scenarios: 200
```

### Cost Optimization

Design cost-effective scenarios:

```yaml
# Expensive: Multiple providers, long conversations
turns:
  - name: "Full Conversation Flow"
    providers: [gpt-4o, claude-opus, gemini-pro]
    turns: [10 multi-turn exchanges]
    # Cost: ~$0.50 per run

# Optimized: Targeted testing
turns:
  - name: "Critical Path Only"
    providers: [gpt-4o-mini]
    turns: [3 key exchanges]
    # Cost: ~$0.05 per run
```

**Strategies:**
- Use mock providers for structure validation
- Use cheaper models (mini/flash) for regression tests
- Reserve expensive models for critical tests
- Batch similar tests together

## Maintenance Patterns

### Versioning Scenarios

Track scenario changes:

```yaml
# Scenario metadata
metadata:
  version: "2.1"
  last_updated: "2024-01-15"
  author: "alice@example.com"
  changelog:
    - version: "2.1"
      date: "2024-01-15"
      changes: "Added tone validation"
    - version: "2.0"
      date: "2024-01-01"
      changes: "Restructured multi-turn flow"
```

### Deprecation Strategy

Handle outdated tests:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: legacy-greeting-test

spec:
  task_type: test
  description: "Legacy Greeting Test"
  
    deprecated: true
    deprecated_reason: "Replaced by greeting-v2.yaml"
    skip: true
    
  - name: "Current Greeting Test"
    tags: [active, v2]
```

### DRY (Don't Repeat Yourself)

Use templates and inheritance:

```yaml
# Base template
templates:
  support_base: &support_base
    tags: [customer-support]
    context:
      department: "support"
    assertions: &support_expected
      - type: llm_judge
        params:
          criteria: "Response is helpful and supportive"
          judge_provider: "openai/gpt-4o-mini"
          message: "Must be helpful"
      - type: llm_judge
        params:
          criteria: "Response has positive sentiment"
          judge_provider: "openai/gpt-4o-mini"
          message: "Must be positive"

# Inherit template
turns:
  - name: "Billing Support"
    <<: *support_base
    turns:
      - role: user
        content: "Question about my bill"
        assertions:
          <<: *support_expected
          - type: content_includes
            params:
              patterns: "billing"
```

## Anti-Patterns to Avoid

### ❌ God Scenarios

```yaml
# Too much in one scenario
turns:
turns:
      # 50+ turns testing unrelated features
```

**Fix:** Break into focused scenarios

### ❌ Flaky Assertions

```yaml
# Unreliable tests - too rigid
assertions:
  - type: content_matches
    params:
      pattern: "^Exactly this format$"
      message: "Exact format required - LLMs vary formatting"
```

**Fix:** Use flexible assertions
```yaml
# Better - flexible validation
assertions:
  - type: content_includes
    params:
      patterns: ["key", "terms"]
      message: "Must contain key terms"
  - type: llm_judge
    params:
      criteria: "Response follows expected format generally"
      judge_provider: "openai/gpt-4o-mini"
      message: "Should follow general format"
```

### ❌ Missing Context

```yaml
# Unclear purpose
turns:
turns:
      - role: user
        content: "something"
```

**Fix:** Add descriptive names and tags

### ❌ Hardcoded Data

```yaml
# Brittle test data
turns:
  - role: user
    content: "My order is #12345 placed on 2024-01-01 for $99.99"
```

**Fix:** Use fixtures and context

## Best Practices Summary

1. **One test, one purpose**: Each scenario tests a specific behavior
2. **Use descriptive names**: Make intent clear
3. **Tag appropriately**: Enable filtering and organization
4. **Layer assertions**: From structure to business logic
5. **Test edges**: Cover happy path and edge cases
6. **Manage context**: Use fixtures for reusability
7. **Design for maintenance**: Version, document, refactor
8. **Balance cost and coverage**: Optimize test execution
9. **Think in conversations**: Model real user interactions
10. **Embrace variability**: Write robust assertions for LLM behavior

## Further Reading

- **[Testing Philosophy](testing-philosophy)** - Why we test LLMs this way
- **[Validation Strategies](validation-strategies)** - Choosing assertions
- **[Provider Comparison](provider-comparison)** - Testing across providers
- **[How-To: Write Scenarios](../how-to/write-scenarios)** - Practical guide
