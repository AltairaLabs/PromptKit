---
layout: default
title: "Tutorial 1: Your First Test"
nav_order: 1
parent: Arena Tutorials
grand_parent: PromptArena
---

# Tutorial 1: Your First Test

Learn the basics of PromptArena by creating and running your first LLM test.

## What You'll Learn

- Install PromptArena
- Create a basic configuration
- Write your first test scenario
- Configure an LLM provider
- Run tests and review results

## Prerequisites

- Go 1.23 or later installed
- An OpenAI API key (free tier works)

## Step 1: Install PromptArena

```bash
# Clone the repository
git clone https://github.com/altairalabs/promptkit.git
cd promptkit

# Install Arena
make install-arena

# Verify installation
promptarena --help
```

You should see the PromptArena command help.

## Step 2: Create Your Test Project

```bash
# Create a new directory
mkdir my-first-test
cd my-first-test

# Create the directory structure
mkdir -p prompts providers scenarios
```

## Step 3: Create a Prompt Configuration

Create `prompts/greeter.yaml`:

```yaml
version: "1.0"
task_type: greeting

system_prompt: |
  You are a friendly assistant who greets users warmly.
  Keep responses brief and welcoming.

user_prompt_template: |
  User message: {% raw %}{{.UserMessage}}{% endraw %}
```

**What's happening here?**
- `task_type`: Identifies this prompt configuration (we'll reference it later)
- `system_prompt`: Instructions for the LLM
- `user_prompt_template`: Template for user messages (Go template syntax)

## Step 4: Configure a Provider

Create `providers/openai.yaml`:

```yaml
version: "1.0"
type: openai
model: gpt-4o-mini
region: us

parameters:
  temperature: 0.7
  max_tokens: 150

auth:
  api_key_env: OPENAI_API_KEY
```

**What's happening here?**
- `type`: The provider type (openai, anthropic, google, etc.)
- `model`: Specific model to use
- `parameters`: Model settings
- `auth`: How to authenticate (reads from environment variable)

## Step 5: Set Your API Key

```bash
# Set the OpenAI API key
export OPENAI_API_KEY="sk-your-api-key-here"

# Or add to your shell profile (~/.zshrc or ~/.bashrc)
echo 'export OPENAI_API_KEY="sk-your-key"' >> ~/.zshrc
source ~/.zshrc
```

## Step 6: Write Your First Test Scenario

Create `scenarios/greeting-test.yaml`:

```yaml
version: "1.0"
task_type: greeting  # Links to prompts/greeter.yaml

test_cases:
  - name: "Basic Greeting"
    tags: [basic, greeting]
    
    turns:
      - user: "Hello!"
        expected:
          - type: contains
            value: ["hello", "hi", "greet"]
          
          - type: max_length
            value: 100
      
      - user: "How are you?"
        expected:
          - type: contains
            value: ["good", "well", "great"]
          
          - type: sentiment
            value: positive
```

**What's happening here?**
- `task_type`: Links to the prompt configuration
- `test_cases`: List of tests to run
- `turns`: Conversation exchanges
- `expected`: Assertions to validate responses

## Step 7: Create Main Configuration

Create `arena.yaml` in your project root:

```yaml
version: "1.0"

prompts:
  - path: ./prompts

providers:
  - path: ./providers

scenarios:
  - path: ./scenarios
```

This tells Arena where to find your configuration files.

## Step 8: Run Your First Test

```bash
promptarena run
```

You should see output like:

```
🚀 PromptArena Starting...

Loading configuration...
  ✓ Loaded 1 prompt config
  ✓ Loaded 1 provider
  ✓ Loaded 1 scenario

Running tests...
  ✓ Basic Greeting - Turn 1 [openai-gpt4o-mini] (1.2s)
  ✓ Basic Greeting - Turn 2 [openai-gpt4o-mini] (1.1s)

Results:
  Total: 2 turns
  Passed: 2
  Failed: 0
  Pass Rate: 100%

Reports generated:
  - out/results.json
```

## Step 9: Review Results

View the JSON results:

```bash
cat out/results.json
```

Or generate an HTML report:

```bash
promptarena run --format html

# Open in browser
open out/report-*.html
```

## Understanding Your First Test

Let's break down what just happened:

### 1. Configuration Loading
Arena loaded your prompt, provider, and scenario files.

### 2. Prompt Assembly
For each turn, Arena:
- Took the system prompt from `greeter.yaml`
- Filled in the user message template
- Sent the complete prompt to OpenAI

### 3. Response Validation
Arena checked each response against your assertions:
- **Contains**: Verified greeting words were present
- **Max Length**: Ensured response wasn't too long
- **Sentiment**: Confirmed positive tone

### 4. Report Generation
Arena saved results in multiple formats for analysis.

## Experiment: Modify the Test

### Add More Assertions

Edit `scenarios/greeting-test.yaml`:

```yaml
turns:
  - user: "Hello!"
    expected:
      - type: contains
        value: ["hello", "hi", "greet"]
      
      - type: max_length
        value: 100
      
      - type: response_time
        max_seconds: 3
      
      - type: tone
        value: friendly
```

Run again:

```bash
promptarena run
```

### Test Edge Cases

Add a new test case:

```yaml
test_cases:
  - name: "Basic Greeting"
    # ... existing test
  
  - name: "Empty Input"
    tags: [edge-case]
    
    turns:
      - user: ""
        expected:
          - type: response_received
            value: true
          
          - type: min_length
            value: 10
```

### Adjust Temperature

Edit `providers/openai.yaml`:

```yaml
parameters:
  temperature: 0.2  # More deterministic
  max_tokens: 150
```

Run and compare:

```bash
promptarena run
```

Lower temperature = more consistent responses.

## Common Issues

### "command not found: promptarena"

```bash
# Ensure Go bin is in PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

### "API key not found"

```bash
# Verify environment variable is set
echo $OPENAI_API_KEY

# Should output: sk-...
```

### "No scenarios found"

Check your `arena.yaml` paths match your directory structure:

```bash
# List your files
ls prompts/
ls providers/
ls scenarios/
```

### "Assertion failed"

This is expected! Assertions validate quality. If one fails:
1. Check the error message in the output
2. Review the actual response in `out/results.json`
3. Adjust your assertions or prompt as needed

## Next Steps

Congratulations! You've run your first LLM test. 

**Continue learning:**
- **[Tutorial 2: Multi-Provider Testing](02-multi-provider.md)** - Test across OpenAI, Claude, and Gemini
- **[Tutorial 3: Multi-Turn Conversations](03-multi-turn.md)** - Build complex dialog flows
- **[How-To: Write Scenarios](../how-to/write-scenarios.md)** - Advanced scenario patterns

**Quick wins:**
- Try different models: `gpt-4o`, `gpt-4o-mini`
- Add more test cases to your scenario
- Generate HTML reports: `promptarena run --format html`

## What's Next?

In Tutorial 2, you'll learn how to test the same scenario across multiple LLM providers (OpenAI, Claude, Gemini) and compare their responses.
