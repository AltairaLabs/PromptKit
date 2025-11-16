
# Customer Support Example

This example demonstrates how to use PromptKit Arena to test a customer support chatbot across multiple LLM providers.

## Scenario

A customer support bot helps users with:
- Product questions
- Order tracking
- Returns and refunds
- Technical troubleshooting

## Files

- `arena.yaml` - Main configuration
- `prompts/support-bot.yaml` - System prompt for the support bot
- `scenarios/support-conversations.yaml` - Test conversation scenarios
- `providers/` - LLM provider configurations

## Running the Example

```bash
cd examples/customer-support
../../tools/arena/bin/promptarena run -c arena.yaml
```

## Expected Outcomes

The test evaluates:
- Tone consistency (professional, helpful, empathetic)
- Accurate information retrieval
- Appropriate escalation handling
- Response quality across providers
