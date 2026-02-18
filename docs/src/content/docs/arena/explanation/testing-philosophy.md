---
title: LLM Testing Philosophy
---
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
assertions:
  - type: content_matches
    params:
      pattern: "^Thank you for contacting AcmeCorp support\\.$"
      message: "Exact wording required"

# ✅ Robust: Behavior validation
assertions:
  - type: content_includes
    params:
      patterns: ["thank", "AcmeCorp", "support"]
      message: "Must acknowledge contact"
  - type: llm_judge
    params:
      criteria: "Response has a professional tone"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be professional"
  - type: llm_judge
    params:
      criteria: "Response has a positive sentiment"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be positive"
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
assertions:
  - type: content_includes   # Correctness
    params:
      patterns: ["30-day return"]
      message: "Must mention return policy"
  
  - type: llm_judge          # Tone
    params:
      criteria: "Response is helpful and supportive"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be helpful"
  
  - type: content_matches    # Safety (negative lookahead)
    params:
      pattern: "^(?!.*(offensive|inappropriate)).*$"
      message: "Must not contain inappropriate content"

  - type: is_valid_json      # Format
    params:
      message: "Response must be valid JSON"
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
turns:
  - name: "Technical Support Context"
    context:
      user_type: "developer"
      urgency: "high"
    turns:
      - role: user
        content: "How do I fix this error?"
        assertions:
          - type: content_includes
            params:
              patterns: ["code", "debug", "solution"]
  
  - name: "General Inquiry Context"
    context:
      user_type: "general"
      urgency: "low"
    turns:
      - role: user
        content: "How do I fix this error?"
        assertions:
          - type: content_includes
            params:
              patterns: ["help", "guide", "steps"]
              message: "Must provide helpful guidance"
          - type: llm_judge
            params:
              criteria: "Response is beginner-friendly and easy to understand"
              judge_provider: "openai/gpt-4o-mini"
              message: "Must be beginner-friendly"
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
assertions:
  - type: content_matches
    params:
      pattern: ".+"
      message: "Response must not be empty"
  - type: is_valid_json  # If expecting JSON
    params:
      message: "Must be valid JSON"

# Level 2: Content
assertions:
  - type: content_includes
    params:
      patterns: ["key information"]
      message: "Must contain key information"
  - type: content_matches
    params:
      pattern: "^.{50,}$"
      message: "Must be at least 50 characters"

# Level 3: Quality
assertions:
  - type: llm_judge
    params:
      criteria: "Response has appropriate sentiment and tone for the context"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must have appropriate quality"
  - type: llm_judge
    params:
      criteria: "Response maintains a professional tone"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be professional"

# Level 4: Custom Business Logic
# Note: Custom validators would need to be implemented as extensions
# For now, use pattern matching or LLM judge for business rules
assertions:
  - type: llm_judge
    params:
      criteria: "Response complies with brand guidelines and voice"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must meet brand compliance"
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
assertions:
  - type: content_includes
    params:
      patterns: ["customer service"]
      message: "Must mention customer service"
  - type: llm_judge
    params:
      criteria: "Response has positive sentiment"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be positive"

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
assertions:
  - type: content_matches
    params:
      pattern: "^Thank you for contacting support\\. Our business hours are 9am-5pm\\.$"
      message: "Exact match required"
```

**Problem**: Brittle. Any wording change breaks the test.

**Better:**
```yaml
assertions:
  - type: content_includes
    params:
      patterns: ["thank", "support", "business hours"]
      message: "Must acknowledge support contact"
  - type: llm_judge
    params:
      criteria: "Response has a professional tone"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be professional"
```

### ❌ Under-Specification

```yaml
# Too loose
assertions:
  - type: content_matches
    params:
      pattern: ".+"
      message: "Must not be empty"
```

**Problem**: Accepts any garbage output.

**Better:**
```yaml
assertions:
  - type: content_matches
    params:
      pattern: ".+"
      message: "Must not be empty"
  - type: content_includes
    params:
      patterns: ["relevant", "keywords"]
      message: "Must contain relevant content"
  - type: content_matches
    params:
      pattern: "^.{50,}$"
      message: "Must be at least 50 characters"
  - type: llm_judge
    params:
      criteria: "Response is appropriate and helpful for the context"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be appropriate"
```

### ❌ Flaky Tests

```yaml
# Assumes specific response structure
assertions:
  - type: content_matches
    params:
      pattern: "^Hello.*\\nHow can I help\\?$"
      message: "Exact format required"
```

**Problem**: LLMs vary formatting.

**Better:**
```yaml
assertions:
  - type: content_includes
    params:
      patterns: ["hello", "help"]
      message: "Must greet and offer help"
  - type: llm_judge
    params:
      criteria: "Response has a welcoming tone"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be welcoming"
```

### ❌ Testing Implementation, Not Behavior

```yaml
# Tests how, not what - too implementation-focused
assertions:
  - type: tools_called
    params:
      tools: ["calculate"]
      message: "Must use calculator tool"

conversation_assertions:
  - type: tool_calls_with_args
    params:
      tool: "calculate"
      expected_args:
        operation: "multiply"
        x: 2
        y: 2
      message: "Must pass exact args"
```

**Problem**: Couples test to implementation details.

**Better:**
```yaml
# Tests outcome - focuses on behavior
assertions:
  - type: content_includes
    params:
      patterns: ["4"]
      message: "Must provide correct answer"
  - type: llm_judge
    params:
      criteria: "Response correctly states that 2 times 2 equals 4"
      judge_provider: "openai/gpt-4o-mini"
      message: "Must be factually correct"
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
  max_cost_per_run: 0.50   # $0.50 per test run
  min_scenarios: 50        # At least 50 scenarios
```

## Testing in Production

### A/B Testing LLM Changes

```yaml
# Test new prompt vs. old prompt
turns:
  - name: "Baseline Prompt"
    prompt_version: "v1.0"
    baseline: true
  
  - name: "Candidate Prompt"
    prompt_version: "v2.0"
    compare_to_baseline: true
    improvement_threshold: 0.05  # 5% better
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: requires-human-review

spec:
  task_type: test
  description: "Requires Human Review"
  
    tags: [human-review]
    
    turns:
      - role: user
        content: "Complex ethical question"
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

- **[Scenario Design Principles](scenario-design)** - How to structure effective test scenarios
- **[Provider Comparison Guide](provider-comparison)** - Understanding provider differences
- **[Validation Strategies](validation-strategies)** - Choosing the right assertions
