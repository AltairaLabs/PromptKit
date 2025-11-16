---
layout: default
title: For Prompt Engineers
parent: Getting Started
nav_order: 1
---

# Getting Started as a Prompt Engineer

**Goal**: Test and validate LLM prompts systematically across multiple providers.

**Tool**: PromptArena

**Time to Success**: 5-10 minutes

---

## What You'll Accomplish

By the end of this guide, you'll have:
- ✅ Arena installed and configured
- ✅ Run your first prompt test
- ✅ Compared responses across providers
- ✅ Generated a test report

---

## Prerequisites

- Go 1.22 or later installed
- API keys for at least one LLM provider (OpenAI, Anthropic, or Google)
- Basic understanding of command-line tools

---

## Step 1: Install PromptArena

```bash
# Clone the repository
git clone https://github.com/AltairaLabs/PromptKit.git
cd PromptKit

# Build Arena
cd tools/arena
go build -o promptarena .

# Verify installation
./promptarena --version
```

Or install from the root using Make:

```bash
make build-arena
./bin/promptarena --version
```

---

## Step 2: Set Up Your API Keys

Create a `.env` file or set environment variables:

```bash
export OPENAI_API_KEY="your-key-here"
export ANTHROPIC_API_KEY="your-key-here"
export GOOGLE_API_KEY="your-key-here"
```

---

## Step 3: Create Your First Test Scenario

Create a file called `my-first-test.yaml`:

```yaml
name: "My First Arena Test"
description: "Testing a simple greeting prompt"

scenarios:
  - name: "Simple Greeting"
    description: "Test basic greeting response"
    
    providers:
      - openai
      - anthropic
    
    messages:
      - role: system
        content: "You are a friendly assistant."
      - role: user
        content: "Say hello and introduce yourself in one sentence."
    
    assertions:
      - type: not_empty
        description: "Response should not be empty"
      - type: max_length
        value: 200
        description: "Response should be concise"
```

---

## Step 4: Run Your First Test

```bash
./promptarena test my-first-test.yaml
```

You'll see output comparing responses from different providers, with validation results.

---

## Step 5: Review the Results

Arena generates:
- Console output with pass/fail status
- Detailed comparison of provider responses
- Performance metrics (tokens, latency, cost)
- HTML report (optional)

```bash
# Generate an HTML report
./promptarena test my-first-test.yaml --report html --output report.html
```

---

## What's Next?

Now that you've run your first test, explore more capabilities:

### 📚 **Tutorials** (Hands-on Learning)
- [Multi-Provider Testing](/arena/tutorials/02-multi-provider/) - Compare providers systematically
- [Multi-Turn Conversations](/arena/tutorials/03-multi-turn/) - Test conversation flows
- [MCP Tool Integration](/arena/tutorials/04-mcp-tools/) - Test prompts with tool calling
- [CI/CD Integration](/arena/tutorials/05-ci-integration/) - Automate testing in pipelines

### 🔧 **How-To Guides** (Specific Tasks)
- [Write Test Scenarios](/arena/how-to/write-scenarios/) - Scenario best practices
- [Configure Providers](/arena/how-to/configure-providers/) - Provider setup
- [Use Mock Providers](/arena/how-to/use-mock-providers/) - Test without API calls
- [Validate Outputs](/arena/how-to/validate-outputs/) - Assertion strategies
- [Integrate CI/CD](/arena/how-to/integrate-ci-cd/) - GitHub Actions, GitLab CI

### 💡 **Concepts** (Understanding)
- [Testing Philosophy](/arena/explanation/testing-philosophy/) - Why test prompts?
- [Scenario Design](/arena/explanation/scenario-design/) - Effective test design
- [Provider Comparison](/arena/explanation/provider-comparison/) - Evaluate providers

### 📖 **Reference** (Look Up Details)
- [CLI Commands](/arena/reference/cli-commands/) - Complete command reference
- [Config Schema](/arena/reference/config-schema/) - Configuration options
- [Assertions](/arena/reference/assertions/) - All assertion types
- [Validators](/arena/reference/validators/) - Built-in validators

---

## Common Use Cases for Prompt Engineers

### Testing Different Prompt Variations
```yaml
scenarios:
  - name: "Formal Tone"
    messages:
      - role: system
        content: "You are a formal, professional assistant."
  
  - name: "Casual Tone"
    messages:
      - role: system
        content: "You are a friendly, casual assistant."
```

### Validating Consistency
```yaml
assertions:
  - type: semantic_similarity
    reference: "Expected response pattern"
    threshold: 0.8
```

### Comparing Provider Costs
```yaml
# Arena automatically tracks cost per scenario
# View in the generated report
```

---

## Troubleshooting

### API Key Not Found
```bash
# Verify environment variables are set
echo $OPENAI_API_KEY
```

### Provider Connection Failed
- Check your API key is valid
- Verify network connectivity
- Check provider status pages

### Tests Taking Too Long
- Use mock providers for local testing
- Reduce the number of providers in test runs
- Use `--parallel` flag for concurrent execution

---

## Join the Community

- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Examples**: [Arena Examples](/arena/examples/)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)

---

## Related Guides

- **For Developers**: [SDK Getting Started](/getting-started/app-developer/) - Build apps with tested prompts
- **For DevOps**: [PackC Getting Started](/getting-started/devops-engineer/) - Package prompts for production
- **Complete Workflow**: [End-to-End Guide](/getting-started/complete-workflow/) - See all tools together
