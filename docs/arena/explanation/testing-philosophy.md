---
layout: default
title: LLM Testing Philosophy
nav_order: 1
parent: Arena Explanation
grand_parent: PromptArena
---

# LLM Testing Philosophy

Understanding the principles and rationale behind PromptArena's approach to LLM testing.

## Why Test LLMs Differently?

Traditional software testing assumes deterministic behavior: given the same input, you get the same output. LLMs break this assumption.

### The LLM Testing Challenge

**Traditional Testing:**
```
input("2+2") → output("4")  // Always
```

**LLM Testing:**
```
input("Greet the user") → output("Hello! How can I help?")
                        → output("Hi there! What can I do for you?")
                        → output("Greetings! How may I assist you today?")
```

Each response is valid but different. This requires a fundamentally different testing approach.

## Core Testing Principles

### 1. Behavioral Testing Over Exact Matching

Instead of testing for exact outputs, test for desired behaviors:

```yaml
# ❌ Brittle: Exact match
expected:
  - type: exact_match
    value: "Thank you for contacting AcmeCorp support."

# ✅ Robust: Behavior validation
expected:
  - type: contains
    value: ["thank", "AcmeCorp", "support"]
  - type: tone
    value: professional
  - type: sentiment
    value: positive
```

**Why:** LLMs generate varied responses. Testing behavior allows flexibility while ensuring quality.

### 2. Multi-Dimensional Quality

LLM quality isn't binary (pass/fail). It's multi-dimensional:

- **Correctness**: Factually accurate?
- **Relevance**: Addresses the query?
- **Tone**: Appropriate style?
- **Safety**: No harmful content?
- **Consistency**: Maintains context?
- **Performance**: Fast enough?

```yaml
expected:
  - type: contains           # Correctness
    value: "30-day return"
  
  - type: sentiment          # Tone
    value: helpful
  
  - type: not_contains       # Safety
    value: ["offensive", "inappropriate"]
  
  - type: references_previous # Consistency
    value: true
  
  - type: response_time      # Performance
    max_seconds: 2
```

### 3. Comparative Testing

Since absolute correctness is elusive, compare:
- **Across providers**: OpenAI vs. Claude vs. Gemini
- **Across versions**: GPT-4 vs. GPT-4o-mini
- **Across time**: Regression detection
- **Against baselines**: Human evaluation benchmarks

```yaml
# Test same scenario across providers
providers: [openai-gpt4, claude-sonnet, gemini-pro]

# Compare results
# Which handles edge cases better?
# Which is faster?
# Which is more cost-effective?
```

### 4. Contextual Validation

Context matters in LLM testing:

```yaml
# Same question, different contexts
test_cases:
  - name: "Technical Support Context"
    context:
      user_type: "developer"
      urgency: "high"
    turns:
      - user: "How do I fix this error?"
        expected:
          - type: contains
            value: ["code", "debug", "solution"]
  
  - name: "General Inquiry Context"
    context:
      user_type: "general"
      urgency: "low"
    turns:
      - user: "How do I fix this error?"
        expected:
          - type: contains
            value: ["help", "guide", "steps"]
          - type: tone
            value: beginner-friendly
```

### 5. Failure is Data

In LLM testing, failures aren't just bugs—they're learning opportunities:

- **Pattern detection**: What types of queries fail?
- **Edge case discovery**: Where do models struggle?
- **Quality tracking**: How does performance change over time?
- **Provider insights**: Which model handles what best?

## Testing Strategies

### Layered Testing Pyramid

```
         ┌─────────────┐
         │  Exploratory │  Manual testing, edge cases
         │   Testing    │
         ├─────────────┤
         │ Integration  │  Multi-turn, complex scenarios
         │    Tests     │
         ├─────────────┤
         │  Scenario    │  Single-turn, common patterns
         │   Tests      │
         ├─────────────┤
         │   Smoke      │  Basic functionality, mock providers
         │   Tests      │
         └─────────────┘
```

**Implementation:**

1. **Smoke Tests** (Fast, Mock)
   - Validate configuration
   - Test scenario structure
   - Verify assertions work
   - Run in < 30 seconds

2. **Scenario Tests** (Common Cases)
   - Core user journeys
   - Expected inputs
   - Standard behaviors
   - Run in < 5 minutes

3. **Integration Tests** (Complex)
   - Multi-turn conversations
   - Tool calling
   - Edge cases
   - Run in < 20 minutes

4. **Exploratory** (Human-in-loop)
   - Adversarial testing
   - Creative edge cases
   - Quality assessment
   - Ongoing

### Progressive Validation

Start simple, add complexity:

```yaml
# Level 1: Structural
expected:
  - type: response_received
  - type: not_empty
  - type: valid_format

# Level 2: Content
expected:
  - type: contains
    value: "key information"
  - type: min_length
    value: 50

# Level 3: Quality
expected:
  - type: sentiment
    value: appropriate
  - type: tone
    value: professional

# Level 4: Custom Business Logic
expected:
  - type: custom
    validator: brand_compliance
  - type: custom
    validator: legal_safety
```

## Design Decisions

### Why PromptPack Format?

PromptArena uses the PromptPack specification for test scenarios. Why?

**Portability**: Test scenarios work across:
- Different testing tools
- Different providers
- Different environments

**Version Control**: YAML format means:
- Git-friendly diffs
- Code review workflows
- Change tracking

**Human Readable**: Non-developers can:
- Write test scenarios
- Review test cases
- Understand failures

### Why Provider Abstraction?

PromptArena abstracts provider differences:

```yaml
# Same scenario, different providers
providers:
  - type: openai
    model: gpt-4o
  - type: anthropic
    model: claude-3-5-sonnet
  - type: google
    model: gemini-1.5-pro
```

**Benefits:**
- Test portability across providers
- Easy provider switching
- Cost optimization
- Vendor independence

### Why Declarative Assertions?

Instead of code, use declarations:

```yaml
# Declarative (PromptArena)
expected:
  - type: contains
    value: "customer service"
  - type: sentiment
    value: positive

# vs. Imperative (traditional)
# assert "customer service" in response
# assert analyze_sentiment(response) == "positive"
```

**Advantages:**
- Non-programmers can write tests
- Consistent validation across scenarios
- Easier to maintain
- Better reporting

### Why Mock Providers?

Mock providers enable:

1. **Fast Development**: Test configuration without API calls
2. **Cost Control**: Iterate without spending
3. **Deterministic Testing**: Predictable responses
4. **Offline Development**: Work without internet
5. **CI/CD Efficiency**: Fast pipeline validation

```bash
# Validate structure (< 10 seconds, $0)
promptarena run --mock-provider

# Validate quality (~ 5 minutes, ~$0.05)
promptarena run --provider openai-gpt4o-mini
```

## Anti-Patterns to Avoid

### ❌ Over-Specification

```yaml
# Too rigid
expected:
  - type: exact_match
    value: "Thank you for contacting us. A support representative will assist you shortly. Our business hours are Monday-Friday 9AM-5PM EST."
```

**Problem**: Brittle. Any wording change breaks the test.

**Better:**
```yaml
expected:
  - type: contains
    value: ["thank", "support", "business hours"]
  - type: tone
    value: professional
```

### ❌ Under-Specification

```yaml
# Too loose
expected:
  - type: response_received
```

**Problem**: Accepts any garbage output.

**Better:**
```yaml
expected:
  - type: response_received
  - type: contains
    value: "relevant keywords"
  - type: min_length
    value: 50
  - type: sentiment
    value: appropriate
```

### ❌ Flaky Tests

```yaml
# Assumes specific response structure
expected:
  - type: regex
    value: "^Hello.*\nHow can I help\?$"
```

**Problem**: LLMs vary formatting.

**Better:**
```yaml
expected:
  - type: contains
    value: ["hello", "help"]
  - type: sentiment
    value: welcoming
```

### ❌ Testing Implementation, Not Behavior

```yaml
# Tests how, not what
expected:
  - type: tool_called
    value: "calculate"
  - type: tool_args
    value: {operation: "multiply", x: 2, y: 2}
```

**Problem**: Couples test to implementation.

**Better:**
```yaml
# Tests outcome
expected:
  - type: contains
    value: "4"
  - type: correctness
    expected_result: "4"
```

## Quality Metrics

### What to Measure

**Primary Metrics:**
- **Pass Rate**: Percentage of assertions passing
- **Response Time**: Latency of responses
- **Cost**: API spending per test run
- **Coverage**: Scenarios tested vs. total scenarios

**Secondary Metrics:**
- **Failure Patterns**: Which types of tests fail most?
- **Provider Comparison**: Which model performs best?
- **Regression Detection**: Are we improving or degrading?
- **Edge Case Coverage**: How many corner cases tested?

### Setting Thresholds

```yaml
# Quality gates
quality_gates:
  min_pass_rate: 0.95      # 95% of assertions must pass
  max_response_time: 3     # 3 seconds max
  max_cost_per_run: 0.50   # $0.50 per test run
  min_scenarios: 50        # At least 50 scenarios
```

## Testing in Production

### A/B Testing LLM Changes

```yaml
# Test new prompt vs. old prompt
test_cases:
  - name: "Baseline Prompt"
    prompt_version: "v1.0"
    baseline: true
  
  - name: "Candidate Prompt"
    prompt_version: "v2.0"
    compare_to_baseline: true
    improvement_threshold: 0.05  # 5% better
```

### Monitoring and Alerting

```yaml
# Continuous testing
schedule: "*/6 * * * *"  # Every 6 hours

alerts:
  - condition: pass_rate < 0.90
    action: notify_team
  
  - condition: response_time > 5
    action: page_oncall
  
  - condition: cost > daily_budget
    action: disable_tests
```

## The Human Factor

### When to Use Human Evaluation

LLMs require human judgment for:
- **Subjective quality**: Is this response "good"?
- **Creative content**: Is this engaging/interesting?
- **Nuanced errors**: Technically correct but contextually wrong
- **Benchmark creation**: Ground truth for automated tests

**Hybrid Approach:**
```
Human Eval → Ground Truth → Automated Tests → Continuous Validation
```

### Human-in-the-Loop Testing

```yaml
test_cases:
  - name: "Requires Human Review"
    tags: [human-review]
    
    turns:
      - user: "Complex ethical question"
        human_evaluation:
          required: true
          criteria:
            - appropriateness
            - thoughtfulness
            - ethical_handling
```

## Conclusion

LLM testing is fundamentally different from traditional testing:

- **Embrace non-determinism**: Test behaviors, not exact outputs
- **Think multi-dimensionally**: Quality has many facets
- **Compare relatively**: Benchmark against alternatives
- **Iterate continuously**: Quality improves over time
- **Balance automation and human judgment**: Both are essential

PromptArena embodies these principles, providing a framework for robust, maintainable LLM testing that scales from development to production.

## Further Reading

- **[Scenario Design Principles](scenario-design.md)** - How to structure effective test scenarios
- **[Provider Comparison Guide](provider-comparison.md)** - Understanding provider differences
- **[Validation Strategies](validation-strategies.md)** - Choosing the right assertions
