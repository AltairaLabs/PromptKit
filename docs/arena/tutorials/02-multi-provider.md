---
layout: docs
title: "Tutorial 2: Multi-Provider Testing"
nav_order: 2
parent: Arena Tutorials
grand_parent: PromptArena
---

# Tutorial 2: Multi-Provider Testing

Learn how to test the same scenario across multiple LLM providers and compare their responses.

## What You'll Learn

- Configure multiple LLM providers (OpenAI, Claude, Gemini)
- Run the same test across all providers
- Compare provider responses
- Understand provider-specific behaviors

## Prerequisites

- Completed [Tutorial 1: Your First Test](01-first-test.md)
- API keys for providers you want to test (at least 2)

## Why Multi-Provider Testing?

Different LLM providers have unique strengths:
- **Response style**: Formal vs. conversational
- **Accuracy**: Factual correctness varies
- **Speed**: Response time differences
- **Cost**: Pricing varies significantly
- **Capabilities**: Tool calling, vision, etc.

Testing across providers helps you:
- Choose the best model for your use case
- Validate consistency across providers
- Build fallback strategies
- Optimize cost vs. quality

## Step 1: Get API Keys

You'll need API keys for the providers you want to test:

### OpenAI
Visit [platform.openai.com](https://platform.openai.com/api-keys)

### Anthropic (Claude)
Visit [console.anthropic.com](https://console.anthropic.com/)

### Google (Gemini)
Visit [aistudio.google.com](https://aistudio.google.com/app/apikey)

## Step 2: Set Up Environment

```bash
# Add all API keys to your environment
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GOOGLE_API_KEY="..."

# Or add to ~/.zshrc for persistence
cat >> ~/.zshrc << 'EOF'
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GOOGLE_API_KEY="..."
EOF

source ~/.zshrc
```

## Step 3: Configure Multiple Providers

Create provider configurations:

### OpenAI

`providers/openai.yaml`:

```yaml
version: "1.0"
type: openai
model: gpt-4o-mini
alias: openai-mini

parameters:
  temperature: 0.7
  max_tokens: 500

auth:
  api_key_env: OPENAI_API_KEY
```

### Anthropic Claude

`providers/claude.yaml`:

```yaml
version: "1.0"
type: anthropic
model: claude-3-5-sonnet-20241022
alias: claude-sonnet

parameters:
  temperature: 0.7
  max_tokens: 500

auth:
  api_key_env: ANTHROPIC_API_KEY
```

### Google Gemini

`providers/gemini.yaml`:

```yaml
version: "1.0"
type: google
model: gemini-1.5-flash
alias: gemini-flash

parameters:
  temperature: 0.7
  max_tokens: 500

auth:
  api_key_env: GOOGLE_API_KEY
```

## Step 4: Create a Comparison Test

Create `scenarios/customer-support.yaml`:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Product Inquiry"
    tags: [customer-service, comparison]
    
    turns:
      - user: "I'm having trouble logging into my account. Can you help?"
        expected:
          # Common expectations for ALL providers
          - type: contains
            value: ["account", "login", "help"]
          
          - type: sentiment
            value: positive
          
          - type: tone
            value: helpful
          
          - type: max_length
            value: 300
          
          - type: response_time
            max_seconds: 5
      
      - user: "I've tried resetting my password but didn't receive an email."
        expected:
          - type: contains
            value: ["email", "spam", "check"]
          
          - type: sentiment
            value: empathetic
```

## Step 5: Update Arena Configuration

Edit `arena.yaml`:

```yaml
version: "1.0"

prompts:
  - path: ./prompts

providers:
  - path: ./providers/openai.yaml
  - path: ./providers/claude.yaml
  - path: ./providers/gemini.yaml

scenarios:
  - path: ./scenarios
```

## Step 6: Run Multi-Provider Tests

```bash
# Run tests across ALL configured providers
promptarena run
```

Output:

```
🚀 PromptArena Starting...

Loading configuration...
  ✓ Loaded 1 prompt config
  ✓ Loaded 3 providers (openai-mini, claude-sonnet, gemini-flash)
  ✓ Loaded 1 scenario

Running tests (3 providers × 1 scenario × 2 turns = 6 test executions)...
  ✓ Product Inquiry - Turn 1 [openai-mini] (1.2s)
  ✓ Product Inquiry - Turn 1 [claude-sonnet] (1.5s)
  ✓ Product Inquiry - Turn 1 [gemini-flash] (0.8s)
  ✓ Product Inquiry - Turn 2 [openai-mini] (1.3s)
  ✓ Product Inquiry - Turn 2 [claude-sonnet] (1.4s)
  ✓ Product Inquiry - Turn 2 [gemini-flash] (0.9s)

Results by Provider:
  openai-mini:     2/2 passed (100%)
  claude-sonnet:   2/2 passed (100%)
  gemini-flash:    2/2 passed (100%)

Overall: 6/6 passed (100%)
```

## Step 7: Generate Comparison Report

```bash
# Generate HTML report with all provider results
promptarena run --format html

# Open the report
open out/report-*.html
```

The HTML report shows side-by-side provider responses for easy comparison.

## Step 8: Test Specific Providers

Sometimes you want to test just one or two providers:

```bash
# Test only OpenAI
promptarena run --provider openai-mini

# Test OpenAI and Claude
promptarena run --provider openai-mini,claude-sonnet

# Test everything except Gemini
promptarena run --provider openai-mini,claude-sonnet
```

## Analyzing Provider Differences

### Response Style Comparison

Create `scenarios/style-test.yaml`:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Response Style Analysis"
    turns:
      - user: "Explain how your product works"
        expected:
          # Check for technical depth
          - type: contains
            value: ["feature", "work", "process"]
          
          # Check response length variations
          - type: min_length
            value: 50
          
          - type: max_length
            value: 500
```

Run and compare:

```bash
promptarena run --scenario style-test --format json

# View detailed responses
cat out/results.json | jq '.results[] | {provider: .provider, response: .response}'
```

### Performance Comparison

Check response times:

```yaml
test_cases:
  - name: "Performance Test"
    turns:
      - user: "Quick question: what's your return policy?"
        expected:
          - type: response_time
            max_seconds: 2  # All providers should respond quickly
```

### Cost Analysis

Different providers have different pricing:

| Provider | Model | Cost (per 1M tokens) |
|----------|-------|---------------------|
| OpenAI | gpt-4o-mini | Input: $0.15, Output: $0.60 |
| Anthropic | claude-3-5-sonnet | Input: $3.00, Output: $15.00 |
| Google | gemini-1.5-flash | Input: $0.075, Output: $0.30 |

Generate a cost report:

```bash
promptarena run --format json

# Calculate costs (example with jq)
cat out/results.json | jq '
  .results | 
  group_by(.provider) | 
  map({
    provider: .[0].provider,
    total_turns: length,
    avg_response_time: (map(.response_time) | add / length)
  })
'
```

## Advanced: Provider-Specific Tests

Test provider-specific features:

### Testing Structured Output (OpenAI)

```yaml
test_cases:
  - name: "JSON Response Test"
    providers: [openai-mini]  # OpenAI-only
    
    turns:
      - user: "Return user info as JSON with name and email"
        expected:
          - type: valid_json
          - type: json_schema
            value:
              type: object
              required: [name, email]
```

### Testing Long Context (Claude)

```yaml
test_cases:
  - name: "Long Context Test"
    providers: [claude-sonnet]  # Claude excels at long context
    
    context:
      long_document: "${fixtures.10k_word_doc}"
    
    turns:
      - user: "Summarize this document"
        expected:
          - type: contains
            value: "key points"
```

## Best Practices

### 1. Keep Parameters Consistent

Use the same temperature and max_tokens across providers for fair comparison:

```yaml
# All providers
parameters:
  temperature: 0.7
  max_tokens: 500
```

### 2. Provider-Agnostic Assertions

Write assertions that work across all providers:

```yaml
# ✅ Good - flexible
expected:
  - type: contains
    value: ["help", "assist", "support"]

# ❌ Avoid - too specific to one provider's style
expected:
  - type: exact_match
    value: "I'd be happy to help you with that!"
```

### 3. Use Aliases

Give providers meaningful names:

```yaml
# providers/openai-creative.yaml
version: "1.0"
type: openai
model: gpt-4o-mini
alias: creative-mini
parameters:
  temperature: 0.9

# providers/openai-precise.yaml
version: "1.0"
type: openai
model: gpt-4o-mini
alias: precise-mini
parameters:
  temperature: 0.1
```

Test configuration variants:

```bash
promptarena run --provider creative-mini,precise-mini
```

### 4. Document Provider Behavior

Add comments to your scenarios:

```yaml
test_cases:
  - name: "Customer Support Response"
    # Note: Claude tends to be more verbose
    # OpenAI more concise, Gemini fastest
    turns:
      - user: "Help with order tracking"
```

## Common Issues

### Missing API Key

```bash
# Verify keys are set
echo $OPENAI_API_KEY
echo $ANTHROPIC_API_KEY
echo $GOOGLE_API_KEY
```

### Provider Not Found

```bash
# Check provider configuration
promptarena config-inspect

# Should list all providers
```

### Rate Limiting

```bash
# Reduce concurrency to avoid rate limits
promptarena run --concurrency 1

# Or test one provider at a time
promptarena run --provider openai-mini
```

## Next Steps

You now know how to test across multiple providers!

**Continue learning:**
- **[Tutorial 3: Multi-Turn Conversations](03-multi-turn.md)** - Build complex dialog flows
- **[Tutorial 4: MCP Tools](04-mcp-tools.md)** - Test tool/function calling
- **[How-To: Configure Providers](../how-to/configure-providers.md)** - Advanced provider setup

**Try this:**
- Add more providers (Azure OpenAI, Groq, etc.)
- Create provider-specific test suites
- Build a cost optimization analysis
- Test the same prompt across different model versions

## What's Next?

In Tutorial 3, you'll learn how to create multi-turn conversation tests that maintain context across multiple exchanges.
