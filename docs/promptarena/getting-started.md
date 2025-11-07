---
layout: page
title: "Getting Started"
permalink: /docs/promptarena/getting-started/
parent: PromptArena
nav_order: 1
---

# Getting Started with PromptArena

This guide will walk you through installing PromptArena and creating your first test suite. By the end, you'll have a working test environment and understand the core workflow.

## Prerequisites

- **Go 1.21+** (for building from source)
- **Git** (for cloning the repository)
- **API Keys** (optional, for testing real providers):
  - OpenAI: `OPENAI_API_KEY`
  - Anthropic: `ANTHROPIC_API_KEY`
  - Google: `GOOGLE_API_KEY`

## Installation

### Option 1: Build from Source (Recommended)

```bash
# Clone the repository
git clone https://github.com/AltairaLabs/PromptKit.git
cd PromptKit

# Build and install PromptArena
make install-tools-user

# Verify installation
promptarena --version
```

This installs `promptarena` to `~/bin/promptarena`.

### Option 2: Build Manually

```bash
cd PromptKit/tools/arena
go build -o promptarena ./cmd/promptarena

# Move to your PATH
sudo mv promptarena /usr/local/bin/
```

### Verify Installation

```bash
promptarena --help
```

You should see the help output with available commands.

## Your First Project

Let's create a simple test suite for an AI assistant.

### Step 1: Create Project Structure

```bash
mkdir my-first-arena
cd my-first-arena

# Create directory structure
mkdir -p prompts scenarios providers
```

Your structure should look like:
```
my-first-arena/
├── arena.yaml          # Main configuration
├── prompts/           # Prompt definitions
├── scenarios/         # Test scenarios
└── providers/         # Provider configurations
```

### Step 2: Define Your Prompt

Create `prompts/assistant.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: helpful-assistant
spec:
  task_type: general
  version: v1.0.0
  description: "A helpful AI assistant that answers questions clearly"

  system_template: |
    You are a helpful AI assistant named Aria.

    Your role:
    - Answer questions accurately and concisely
    - Admit when you don't know something
    - Ask clarifying questions when needed
    - Be friendly and professional

    Guidelines:
    - Keep responses under 3 sentences unless more detail is requested
    - Use simple language
    - Provide examples when helpful

  # Optional: Define validators/guardrails
  validators:
    - type: max_length
      params:
        max_characters: 500
        max_tokens: 150

    - type: banned_words
      params:
        words:
          - "guarantee"
          - "definitely"
          - "100%"
```

**Key Points**:
- `task_type`: Categorizes the prompt (general, support, creative, etc.)
- `system_template`: The system prompt sent to the LLM
- `validators`: Optional runtime guardrails

### Step 3: Create a Test Scenario

Create `scenarios/basic-qa.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-qa
spec:
  task_type: general
  description: "Test basic question-answering capabilities"

  turns:
    # Turn 1: Simple factual question
    - role: user
      content: "What is the capital of France?"
      assertions:
        - type: content_includes
          params:
            text: "Paris"
            message: "Should correctly identify Paris as the capital"

    # Turn 2: Follow-up question
    - role: user
      content: "What's the population?"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(million|population)"
            message: "Should provide population information"

    # Turn 3: Admission of uncertainty
    - role: user
      content: "What will the weather be like there next Tuesday?"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(don't know|cannot|can't|unable to|don't have access)"
            message: "Should admit limitations for real-time data"

  # Optional: Provide context about the scenario
  context:
    goal: "Test factual Q&A and handling of questions requiring real-time data"
    user_type: "general user"
```

**Key Points**:
- `turns`: Sequential conversation turns
- `role`: Either "user" or "assistant"
- `assertions`: Checks that run after each turn
- User turns trigger LLM responses; assistant turns are optional for multi-turn context

### Step 4: Configure a Provider

For development, start with a mock provider:

Create `providers/mock.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: mock-dev
spec:
  type: mock
  model: mock-model
  defaults:
    temperature: 0.7
    max_tokens: 500
```

**For real testing**, create `providers/openai.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4o-mini
spec:
  type: openai
  model: gpt-4o-mini
  defaults:
    temperature: 0.7
    max_tokens: 500
    seed: 42  # For reproducibility

# API key loaded from environment: OPENAI_API_KEY
```

### Step 5: Create Arena Configuration

Create `arena.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-first-arena
spec:
  # Link prompts
  prompt_configs:
    - id: assistant
      file: prompts/assistant.yaml

  # Configure providers to test
  providers:
    - file: providers/openai.yaml
    # - file: providers/mock.yaml  # Uncomment for mock testing

  # Define scenarios to run
  scenarios:
    - file: scenarios/basic-qa.yaml

  # Global defaults
  defaults:
    temperature: 0.7
    max_tokens: 1500
    seed: 42
    concurrency: 1

    # Output configuration
    output:
      dir: out
      formats: ["json", "html", "markdown"]
      html:
        file: report.html
      json:
        file: results.json
      markdown:
        file: report.md

    # Failure conditions
    fail_on:
      - assertion_failure
      - provider_error
      - timeout
```

**Key Sections**:
- `prompt_configs`: Maps prompt IDs to files
- `providers`: Lists providers to test against
- `scenarios`: Lists test scenarios to run
- `defaults`: Global settings and output configuration

### Step 6: Run Your Tests

```bash
# Set API key (if using real provider)
export OPENAI_API_KEY="your-key-here"

# Run tests
promptarena run arena.yaml

# With verbose output
promptarena run arena.yaml --verbose

# Run specific scenarios only
promptarena run arena.yaml --scenarios basic-qa
```

### Step 7: View Results

```bash
# Open HTML report
open out/report.html

# View JSON results
cat out/results.json

# View markdown report
cat out/report.md
```

The HTML report shows:
- Pass/fail status for each scenario
- Detailed conversation transcripts
- Assertion results
- Token usage and costs
- Performance metrics

## Understanding the Output

### HTML Report Structure

```
Report
├── Summary
│   ├── Total scenarios
│   ├── Pass/fail counts
│   ├── Total tokens used
│   └── Total cost
├── Provider Comparison (if multiple providers)
│   └── Side-by-side results
└── Detailed Results
    └── For each scenario:
        ├── Conversation transcript
        ├── Assertion results
        ├── Token usage
        └── Timing information
```

### Console Output

```
🏟️  PromptArena Test Run
📝 Configuration: arena.yaml
🎯 Scenarios: 1
🤖 Providers: 1

Running scenario: basic-qa
  Provider: openai-gpt4o-mini
    Turn 1 ✅ (3 assertions passed)
    Turn 2 ✅ (1 assertion passed)
    Turn 3 ✅ (1 assertion passed)
  Result: PASS (3/3 turns passed)

Summary:
  Scenarios: 1 passed, 0 failed
  Total assertions: 5 passed, 0 failed
  Tokens used: 450 (input: 200, output: 250)
  Estimated cost: $0.0023
  Duration: 3.2s

✅ All tests passed!
```

## Development Workflow

### Recommended Iteration Cycle

1. **Start with Mock Mode**
   ```yaml
   providers:
     - file: providers/mock.yaml
   ```
   - Fast iteration without API costs
   - Focus on test structure and assertions

2. **Test with Real Provider**
   ```yaml
   providers:
     - file: providers/openai.yaml
   ```
   - Validate actual LLM behavior
   - Capture real responses

3. **Add More Providers**
   ```yaml
   providers:
     - file: providers/openai.yaml
     - file: providers/anthropic.yaml
     - file: providers/gemini.yaml
   ```
   - Compare behavior across models
   - Find the best model for your use case

4. **Expand Scenarios**
   - Add edge cases
   - Test error handling
   - Add multi-turn conversations

### Quick Commands

```bash
# Validate config without running
promptarena validate arena.yaml

# Run with specific provider
promptarena run arena.yaml --providers openai-gpt4o-mini

# Run specific scenarios
promptarena run arena.yaml --scenarios "basic-qa,edge-cases"

# Parallel execution (faster)
promptarena run arena.yaml --parallel 3

# Custom output directory
promptarena run arena.yaml --output results-$(date +%Y%m%d)
```

## Common Patterns

### Testing Multiple Providers

```yaml
# arena.yaml
spec:
  providers:
    - file: providers/openai-gpt4o.yaml
    - file: providers/openai-gpt4o-mini.yaml
    - file: providers/claude-3-5-sonnet.yaml
    - file: providers/gemini-2-0-flash.yaml
```

Run to compare all providers side-by-side.

### Using Environment Variables

```yaml
# providers/openai.yaml
spec:
  type: openai
  model: gpt-4o-mini
  # Uses OPENAI_API_KEY from environment
```

Or override programmatically:
```bash
OPENAI_API_KEY=sk-... promptarena run arena.yaml
```

### Organizing Large Test Suites

```
my-project/
├── arena.yaml
├── prompts/
│   ├── customer-support.yaml
│   ├── content-generation.yaml
│   └── code-assistant.yaml
├── scenarios/
│   ├── support/
│   │   ├── basic-inquiries.yaml
│   │   ├── refunds.yaml
│   │   └── technical-issues.yaml
│   └── content/
│       ├── blog-posts.yaml
│       └── social-media.yaml
└── providers/
    ├── openai-gpt4o.yaml
    ├── claude-sonnet.yaml
    └── gemini-flash.yaml
```

Reference in arena.yaml:
```yaml
scenarios:
  - file: scenarios/support/basic-inquiries.yaml
  - file: scenarios/support/refunds.yaml
  - file: scenarios/content/blog-posts.yaml
```

## Next Steps

Now that you have a working test environment:

1. **[Learn the PromptPack Specification](./promptpack-spec.md)** - Deep dive into the format
2. **[Master Assertions](./assertions.md)** - Write effective test conditions
3. **[Explore Self-Play](./selfplay.md)** - Let AI discover edge cases
4. **[Add Tool Calling](./tools.md)** - Test function calling
5. **[Best Practices](./best-practices.md)** - Tips for production use

## Troubleshooting

### "Command not found: promptarena"

Ensure `~/bin` is in your PATH:
```bash
export PATH="$HOME/bin:$PATH"
echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bashrc  # or ~/.zshrc
```

### "Provider initialization failed"

Check that API keys are set:
```bash
echo $OPENAI_API_KEY
```

Or use mock provider for testing without API keys.

### "Scenario validation failed"

Run validation to see detailed errors:
```bash
promptarena validate arena.yaml
```

### Tests are slow

Use parallel execution:
```bash
promptarena run arena.yaml --parallel 5
```

Or test with fewer providers during development.

---

**Ready to dive deeper?** Continue to [PromptPack Specification](./promptpack-spec.md) to learn the full format.
