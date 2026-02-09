---
title: 'Tutorial 1: Your First Test'
sidebar:
  order: 1
---
Learn the basics of PromptArena by creating and running your first LLM test.

## What You'll Learn

- Install PromptArena
- Create a basic configuration
- Write your first test scenario
- Configure an LLM provider
- Run tests and review results

## Prerequisites

- An OpenAI API key (free tier works)

## Step 1: Install PromptArena

Choose your preferred installation method:

**Option 1: Homebrew (Recommended)**

```bash
brew install promptkit
```

**Option 2: Go Install**

```bash
go install github.com/AltairaLabs/PromptKit/tools/arena@latest
```

**Verify installation:**

```bash
promptarena --version
```

You should see the PromptArena version information.

## Step 2: Create Your Test Project

**The Easy Way: Use the Template Generator**

```bash
# Create a complete test project in seconds
promptarena init my-first-test --quick --provider openai

# Navigate to your project
cd my-first-test
```

That's it! The `init` command created everything you need:

- ✅ Arena configuration (`arena.yaml`)
- ✅ Prompt setup (`prompts/assistant.yaml`)
- ✅ Provider configuration (`providers/openai.yaml`)
- ✅ Sample test scenario (`scenarios/basic-test.yaml`)
- ✅ Environment setup (`.env`)

**The Manual Way: Create Files Step-by-Step**

If you prefer to understand each component:

```bash
# Create a new directory
mkdir my-first-test
cd my-first-test

# Create the directory structure
mkdir -p prompts providers scenarios
```

## Step 3: Create a Prompt Configuration

**If you used `promptarena init`:** You already have `prompts/assistant.yaml`. Feel free to edit it!

**If creating manually:** Create `prompts/greeter.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: greeter

spec:
  task_type: greeting
  
  system_template: |
    You are a friendly assistant who greets users warmly.
    Keep responses brief and welcoming.
```

**What's happening here?**
- `apiVersion` and `kind`: Standard PromptKit resource identifiers
- `metadata.name`: Identifies this prompt configuration (we'll reference it later)
- `spec.task_type`: Categorizes the prompt's purpose
- `spec.system_template`: System instructions sent to the LLM

## Step 4: Configure a Provider

Create `providers/openai.yaml`:

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
    max_tokens: 150
```

**What's happening here?**
- `apiVersion` and `kind`: Standard PromptKit resource identifiers
- `metadata.name`: Friendly name for this provider configuration
- `spec.type`: The provider type (openai, anthropic, gemini)
- `spec.model`: Specific model to use
- `spec.defaults`: Model parameters like temperature and max_tokens
- Authentication uses environment variable `OPENAI_API_KEY` automatically

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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: greeting-test
  labels:
    category: basic

spec:
  task_type: greeting  # Links to prompts/greeter.yaml
  
  turns:
    - role: user
      content: "Hello!"
      assertions:
        - type: content_includes
          params:
            patterns: ["hello"]
            message: "Should include greeting"

        - type: content_matches
          params:
            pattern: "^.{1,100}$"
            message: "Response should be brief"
    
    - role: user
      content: "How are you?"
      assertions:
        - type: content_includes
          params:
            patterns: ["good"]
            message: "Should respond positively"
```

**What's happening here?**
- `apiVersion` and `kind`: Standard PromptKit resource identifiers
- `metadata.name`: Identifies this scenario
- `spec.task_type`: Links to the prompt configuration with matching task_type
- `spec.turns`: Array of conversation exchanges
- `role: user`: Each user turn triggers an LLM response
- `assertions`: Checks to validate the LLM's response

## Step 7: Create Main Configuration

Create `arena.yaml` in your project root:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-first-test

spec:
  prompt_configs:
    - id: greeter
      file: prompts/greeter.yaml
  
  providers:
    - file: providers/openai.yaml
  
  scenarios:
    - file: scenarios/greeting-test.yaml
```

This tells Arena which configurations to load and how to connect them.

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
- **content_includes**: Verified greeting words were present
- **content_matches**: Ensured response matched expected patterns

### 4. Report Generation
Arena saved results in multiple formats for analysis.

## Experiment: Modify the Test

### Add More Assertions

Edit `scenarios/greeting-test.yaml` to add more checks:

```yaml
spec:
  turns:
    - role: user
      content: "Hello!"
      assertions:
        - type: content_includes
          params:
            patterns: ["hello"]

        - type: content_matches
          params:
            pattern: "^.{1,100}$"
            message: "Response should be brief"
```

Run again:

```bash
promptarena run
```

### Test Edge Cases

Create a new scenario file `scenarios/edge-cases.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: edge-cases
  labels:
    category: edge-case

spec:
  task_type: greeting
  
  turns:
    - role: user
      content: ""
      assertions:
        - type: content_matches
          params:
            pattern: "^.{10,}$"
            message: "Response should be at least 10 characters"
```

Add it to `arena.yaml`:

```yaml
spec:
  scenarios:
    - file: scenarios/greeting-test.yaml
    - file: scenarios/edge-cases.yaml
```

### Adjust Temperature

Edit `providers/openai.yaml`:

```yaml
spec:
  defaults:
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
- **[Tutorial 2: Multi-Provider Testing](02-multi-provider)** - Test across OpenAI, Claude, and Gemini
- **[Tutorial 3: Multi-Turn Conversations](03-multi-turn)** - Build complex dialog flows
- **[How-To: Write Scenarios](../how-to/write-scenarios)** - Advanced scenario patterns

**Quick wins:**
- Try different models: `gpt-4o`, `gpt-4o-mini`
- Add more test cases to your scenario
- Generate HTML reports: `promptarena run --format html`

## What's Next?

In Tutorial 2, you'll learn how to test the same scenario across multiple LLM providers (OpenAI, Claude, Gemini) and compare their responses.
