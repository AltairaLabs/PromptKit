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
### [Use guardrails as test signals (the three-role model)](/arena/how-to/guardrails-as-signals/)
Walk through `examples/guardrails-test/`. One primitive enforces in production AND fires as an observable test signal. The canonical demo of the eval / guardrail / assertion bridge.
</div>

<div class="code-example" markdown="1">
### [Generate Mock Responses from Arena Results](/arena/how-to/generate-mock-responses-from-arena/)
Turn recorded Arena runs into mock provider YAML for deterministic, cost-free replays.
</div>

<div class="code-example" markdown="1">
### [Gate model migrations on a regression suite](/arena/how-to/model-migration/)
Walk through `examples/model-migration/`: run the same scenarios against the old and new model side by side. CI exits non-zero if the new model breaks an assertion the old one passed.
</div>

## Voice Testing

<div class="code-example" markdown="1">
### [Set Up Voice Testing with Self-Play](/arena/how-to/setup-voice-testing/)
Configure automated voice testing using duplex streaming and self-play with TTS.
</div>

<div class="code-example" markdown="1">
### [Test a Voice Customer Support Agent](/arena/how-to/voice-customer-support/)
Walk through `examples/voice-refund-demo/`: four scripted personas (hostile, impersonator, anxious, patient) driving a refund agent under voice, with conversation-level assertions on the tools the agent must (and must not) call.
</div>

<div class="code-example" markdown="1">
### [Test a Voice IVR with a Workflow State Machine](/arena/how-to/voice-ivr/)
Walk through `examples/voice-ivr/`: a workflow-driven bank IVR that routes callers via state transitions to self-service or human handoff. Pairs the workflow primitive with the voice harness and asserts the tool-call pattern of each path.
</div>

<div class="code-example" markdown="1">
### [Assert per-turn latency budgets](/arena/how-to/voice-latency-budget/)
Walk through `examples/voice-latency-budget/`: gate every turn against a `max_ms` budget. Arena bridges the assistant message's `LatencyMs` into eval context, so `latency_budget` reads real provider timing with no custom plumbing.
</div>

<div class="code-example" markdown="1">
### [Test voice agents that call tools mid-conversation](/arena/how-to/voice-tool-calls/)
Walk through `examples/duplex-streaming/scenarios/duplex-tools.scenario.yaml`: a busy-professional persona drives a voice agent through weather / calendar / reminder tool calls. Conversation-level assertions catch the tool-call pattern without leaving the audio pipeline.
</div>

<div class="code-example" markdown="1">
### [Red-team a voice agent with safety guardrails](/arena/how-to/voice-red-team/)
Walk through `examples/voice-red-team/`: `pii_leakage` wired as a guardrail in the pack, scenarios assert the firing via `guardrail_triggered`. Same primitive enforces in production AND fires as a test signal — the three-role pattern (eval / guardrail / assertion) end-to-end.
</div>

<div class="code-example" markdown="1">
### [PII-redaction guardrails for voice agents](/arena/how-to/voice-guardrails/)
Walk through `examples/voice-guardrails/`: a focused single-scenario demo of the runtime + test bridge. The `pii_leakage` guardrail replaces the agent's would-be-spoken PII before reaching TTS; the test reads the firing from `validations:` on the recorded message via `guardrail_triggered`.
</div>

<div class="code-example" markdown="1">
### [Run the same scenario across multiple providers](/arena/how-to/voice-bake-off/)
Walk through `examples/voice-bake-off/`: one scenario, two providers, side-by-side report. Adding a provider is one YAML line; per-provider thresholds use `when:` clauses. The fan-out shape stays the same whether you're comparing mocks or real duplex providers.
</div>

<div class="code-example" markdown="1">
### [Test expressive voice personas with characterization tags](/arena/how-to/voice-characterization/)
Walk through the expressive path in `examples/voice-refund-demo/`. Personas opt in with `expressive: true` and emit canonical bracket tags (`[shouts]`, `[whispers]`, `[laughs]`); each TTS provider adapter lowers them into its native dialect (ElevenLabs v3 native, OpenAI instructions, Cartesia emotion, SSML).
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

## Multi-Turn Testing

<div class="code-example" markdown="1">
### [Test agent negotiation with scripted or self-play opponents](/arena/how-to/text-negotiation/)
Walk through `examples/text-negotiation/`: a four-turn rental-price negotiation with conversation-outcome assertions. Default runs deterministically against a mock landlord; the how-to documents the swap to real LLM-driven self-play via `role: selfplay-user` and a persona.
</div>

## Automation

<div class="code-example" markdown="1">
### [Integrate with CI/CD](/arena/how-to/integrate-ci-cd/)
Automate LLM testing in GitHub Actions, GitLab CI, Jenkins, and other pipelines.
</div>

<div class="code-example" markdown="1">
### [Run Arena as a CI quality gate](/arena/how-to/arena-ci-quality-gate/)
Wire `promptarena run --ci` into GitHub Actions as a hard merge gate. Fork-safe defaults, real-provider keys via secrets, threshold-based pass/fail, report uploads for reviewers.
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
