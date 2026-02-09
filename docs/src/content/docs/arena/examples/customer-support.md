---
title: Customer Support Example
description: Example demonstrating customer-support
sidebar:
  order: 100
---
This example demonstrates how to use PromptKit Arena to test a customer support chatbot across multiple LLM providers.

## Scenario

A customer support bot helps users with:
- Product questions
- Order tracking
- Returns and refunds
- Technical troubleshooting

## Files

- `config.arena.yaml` - Main configuration
- `prompts/support-bot.yaml` - System prompt for the support bot
- `scenarios/support-conversations.scenario.yaml` - Test conversation scenarios
- `scenarios/streaming-demo.scenario.yaml` - Demonstrates streaming configuration
- `scenarios/streaming-tools-demo.scenario.yaml` - **Demonstrates streaming with tool calls**
- `tools/example-tool.tool.yaml` - Mock tool for order status lookup
- `providers/` - LLM provider configurations

## Running the Example

```bash
cd examples/customer-support
promptarena run -c config.arena.yaml
```

### Running a Specific Scenario

To run just the streaming-with-tools demo:

```bash
promptarena run -c config.arena.yaml --scenario streaming-tools-demo
```

## Scenarios

### Streaming with Tools Demo

The `streaming-tools-demo` scenario demonstrates the `PredictStreamWithTools` feature where the LLM can stream tokens back while also invoking tools:

1. **Turn 1**: User asks about order status → LLM calls `get_order_status` tool while streaming
2. **Turn 2**: Follow-up question → LLM uses previous tool result in streamed response
3. **Turn 3**: Another order check → LLM calls the tool again
4. **Turn 4**: General question → Streamed response without tool call

This showcases how streaming and tool calls work together seamlessly.

## Expected Outcomes

The test evaluates:
- Tone consistency (professional, helpful, empathetic)
- Accurate information retrieval
- Tool invocation when needed (order status lookups)
- Appropriate escalation handling
- Response quality across providers
