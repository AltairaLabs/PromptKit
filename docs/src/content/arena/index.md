---
title: PromptArena
description: >-
  Comprehensive testing framework for validating LLM prompts across multiple
  providers
docType: guide
order: 2
---
# 🏟️ PromptArena

**Comprehensive testing framework for validating LLM prompts across multiple providers**

---

## What is PromptArena?

PromptArena is a powerful testing tool that helps you:

- **Test prompts systematically** across OpenAI, Anthropic, Google, and more
- **Compare provider performance** side-by-side with detailed metrics
- **Validate conversation flows** with multi-turn testing scenarios
- **Integrate with CI/CD** to catch prompt regressions before production
- **Generate comprehensive reports** with HTML, JSON, and markdown output

---

## Quick Start

```bash
# Install PromptKit (includes PromptArena)
brew install promptkit

# Or with Go
go install github.com/AltairaLabs/PromptKit/tools/arena@latest

# Create your first test
cat > arena.yaml <<EOF
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-first-test

spec:
  providers:
    - path: ./providers/openai.yaml
  
  scenarios:
    - path: ./scenarios/greeting.yaml
EOF

cat > scenarios/greeting.yaml <<EOF
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-greeting

spec:
  turns:
    - role: user
      content: "Hello!"
      assertions:
        - type: content_not_empty
          params:
            message: "Should respond"
EOF

# Run the test
./bin/promptarena run
```

**Next**: [Your First Arena Test Tutorial](/arena/tutorials/01-first-test/)

---

## Documentation by Type

### 📚 Tutorials (Learn by Doing)

Step-by-step guides that teach you Arena through hands-on exercises:

1. [Your First Test](/arena/tutorials/01-first-test/) - Get started in 5 minutes
2. [Multi-Provider Testing](/arena/tutorials/02-multi-provider/) - Compare providers
3. [Multi-Turn Conversations](/arena/tutorials/03-multi-turn/) - Test conversation flows
4. [MCP Tool Integration](/arena/tutorials/04-mcp-tools/) - Test with tool calling
5. [CI/CD Integration](/arena/tutorials/05-ci-integration/) - Automate testing

### 🔧 How-To Guides (Accomplish Specific Tasks)

Focused guides for specific Arena tasks:

- [Installation](/arena/how-to/installation/) - Get Arena running
- [Write Test Scenarios](/arena/how-to/write-scenarios/) - Effective scenario design
- [Configure Providers](/arena/how-to/configure-providers/) - Provider setup
- [Use Mock Providers](/arena/how-to/use-mock-providers/) - Test without API calls
- [Validate Outputs](/arena/how-to/validate-outputs/) - Assertion strategies
- [Customize Reports](/arena/how-to/customize-reports/) - Report formatting
- [Integrate CI/CD](/arena/how-to/integrate-ci-cd/) - GitHub Actions, GitLab CI

### 💡 Explanation (Understand the Concepts)

Deep dives into Arena's design and philosophy:

- [Testing Philosophy](/arena/explanation/testing-philosophy/) - Why test prompts?
- [Scenario Design](/arena/explanation/scenario-design/) - Effective test patterns
- [Provider Comparison](/arena/explanation/provider-comparison/) - Evaluate providers
- [Validation Strategies](/arena/explanation/validation-strategies/) - Assertion best practices

### 📖 Reference (Look Up Details)

Complete technical specifications:

- [CLI Commands](/arena/reference/cli-commands/) - All Arena commands
- [Configuration Schema](/arena/reference/config-schema/) - Config file format
- [Scenario Format](/arena/reference/scenario-format/) - Test scenario structure
- [Assertions](/arena/reference/assertions/) - All assertion types
- [Validators](/arena/reference/validators/) - Built-in validators
- [Output Formats](/arena/reference/output-formats/) - Report formats

---

## Key Features

### Multi-Provider Testing

Test the same prompt across different LLM providers simultaneously:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: cross-provider-test

spec:
  providers:
    - path: ./providers/openai.yaml
    - path: ./providers/claude.yaml
    - path: ./providers/gemini.yaml
  
  scenarios:
    - path: ./scenarios/quantum-test.yaml
      providers: [openai-gpt4, claude-sonnet, gemini-pro]
```

### Rich Assertions

Validate outputs with powerful assertions:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: quantum-test

spec:
  turns:
    - role: user
      content: "Explain quantum computing"
      assertions:
        - type: content_not_empty
          params:
            message: "Should provide response"
        
        - type: content_includes
          params:
            text: "quantum"
            message: "Should mention quantum"
        
        - type: content_max_tokens
          params:
            tokens: 500
            message: "Should be concise"
        
        - type: semantic_similarity
          params:
            reference: "Expected explanation"
            threshold: 0.85
            message: "Should match expected content"
```

### Performance Metrics

Automatically track:

- Response time (latency)
- Token usage (input/output)
- Cost estimation
- Success/failure rates

### CI/CD Integration

Run tests in your pipeline:

```yaml
# .github/workflows/test-prompts.yml
- name: Test Prompts
  run: promptarena run --ci --fail-on-error
```

---

## Use Cases

### For Prompt Engineers

- Develop and refine prompts with confidence
- A/B test different prompt variations
- Ensure consistency across providers
- Track performance over time

### For QA Teams

- Validate prompt quality before deployment
- Catch regressions in prompt behavior
- Test edge cases and failure modes
- Generate test reports for stakeholders

### For ML Ops

- Integrate prompt testing into CI/CD
- Monitor prompt performance
- Compare provider costs and quality
- Automate regression testing

---

## Examples

Real-world Arena testing scenarios:

- [Customer Support Testing](/arena/examples/customer-support/) - Multi-turn support conversations
- [MCP Chatbot Testing](/arena/examples/mcp-chatbot/) - Tool calling validation
- [Guardrails Testing](/arena/examples/guardrails/) - Safety and compliance checks
- [Multi-Provider Comparison](/arena/examples/multi-provider/) - Provider evaluation

---

## Common Workflows

### Development Workflow

1. Write prompt → 2. Create test → 3. Run Arena → 4. Refine → 5. Repeat

### CI/CD Workflow

1. Push changes → 2. Arena runs automatically → 3. Tests must pass → 4. Deploy

### Provider Evaluation

1. Define test suite → 2. Run across providers → 3. Compare results → 4. Choose best

---

## Getting Help

- **Quick Start**: [Getting Started Guide](/getting-started/prompt-engineer/)
- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)
- **Examples**: [Arena Examples](/arena/examples/)

---

## Related Tools

- **PackC**: [Compile tested prompts](/packc/) for production
- **SDK**: [Use tested prompts in applications](/sdk/)
- **Complete Workflow**: [See all tools together](/getting-started/complete-workflow/)
