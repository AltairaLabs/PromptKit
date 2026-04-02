---
title: Arena How-To
sidebar:
  order: 0
---
Practical guides for accomplishing specific tasks with PromptArena.

## Getting Started

<div class="code-example" markdown="1">
### [Install PromptArena](/arena/how-to/installation/)
Set up PromptArena on your system and verify the installation.
</div>

<div class="code-example" markdown="1">
### [Configure Shell Completions](/arena/how-to/shell-completions/)
Enable tab completion for commands, flags, and dynamic values like scenarios and providers.
</div>

<div class="code-example" markdown="1">
### [Use Project Templates](/arena/how-to/use-project-templates/)
Quickly scaffold new test projects with the `promptarena init` command. Includes 6 built-in templates for common use cases like customer support, code generation, content creation, multimodal AI, and MCP integration.
</div>

<div class="code-example" markdown="1">
### [Write Test Scenarios](/arena/how-to/write-scenarios/)
Create and structure test scenarios for LLM testing with the PromptPack format.
</div>

<div class="code-example" markdown="1">
### [Configure LLM Providers](/arena/how-to/configure-providers/)
Set up and manage connections to OpenAI, Anthropic, Google, and other LLM providers.
</div>

## Testing Strategies

<div class="code-example" markdown="1">
### [Use Mock Providers](/arena/how-to/use-mock-providers/)
Test quickly and cost-free with mock providers instead of real LLM APIs.
</div>

<div class="code-example" markdown="1">
### [Validate Outputs](/arena/how-to/validate-outputs/)
Use assertions and custom validators to verify LLM response quality.
</div>

<div class="code-example" markdown="1">
### [Generate Mock Responses from Arena Results](/arena/how-to/generate-mock-responses-from-arena/)
Turn recorded Arena runs into mock provider YAML for deterministic, cost-free replays.
</div>

## Voice Testing

<div class="code-example" markdown="1">
### [Set Up Voice Testing with Self-Play](/arena/how-to/setup-voice-testing/)
Configure automated voice testing using duplex streaming and self-play with TTS.
</div>

## Session Recording

<div class="code-example" markdown="1">
### [Session Recording](/arena/how-to/session-recording/)
Capture detailed session recordings for debugging, replay, and analysis. Export audio tracks, correlate events with annotations, and use recordings for deterministic test replay.
</div>

## Context Management

<div class="code-example" markdown="1">
### [Manage Context](/arena/how-to/manage-context/)
Configure context management and truncation strategies for long conversations, including embedding-based relevance truncation.
</div>

## Tool Integrations

<div class="code-example" markdown="1">
### [Test MCP Tools](/arena/how-to/test-mcp-tools/)
Configure MCP servers in Arena for integration testing with tool filtering, timeouts, and environment variables.
</div>

<div class="code-example" markdown="1">
### [Test A2A Agents](/arena/how-to/test-a2a-agents/)
Test agent-to-agent delegation with mock or remote A2A agents, including authentication, headers, and skill filtering.
</div>

## Automation

<div class="code-example" markdown="1">
### [Integrate with CI/CD](/arena/how-to/integrate-ci-cd/)
Automate LLM testing in GitHub Actions, GitLab CI, Jenkins, and other pipelines.
</div>

## What's the Difference?

**How-to guides** are goal-oriented recipes that show you **how to solve** specific problems:

- ✅ "How do I install Arena?"
- ✅ "How do I configure multiple providers?"
- ✅ "How do I integrate with GitHub Actions?"

Looking for something else?

- **[Tutorials](/arena/tutorials/)** - Step-by-step learning paths for beginners
- **[Explanation](/arena/)** - Understand concepts and design decisions
- **[Reference](/arena/reference/)** - Complete technical specifications and API docs
