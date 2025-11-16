
# Assertions Test Example

This example demonstrates turn-level assertions across three functional areas:

## Scenarios

1. **scripted-turns.yaml** - Scripted conversation turns with `content_includes` and `content_matches` assertions
2. **self-play.yaml** - Self-play mode with assertions on every turn  
3. **tool-usage.yaml** - Tool usage validation with `tools_called` and `tools_not_called` assertions

## Setup

Copy `.env.example` to `.env` and add your API keys:

```bash
cp .env.example .env
# Edit .env and add your OPENAI_API_KEY and GEMINI_API_KEY
```

## Running the Tests

```bash
# Run all scenarios with both providers
./bin/promptarena run -c examples/assertions-test

# Run specific scenario
./bin/promptarena run -c examples/assertions-test --scenario scripted-turns --provider gemini-flash
./bin/promptarena run -c examples/assertions-test --scenario tool-usage --provider openai-mini
./bin/promptarena run -c examples/assertions-test --scenario self-play --provider gemini-flash
```

## Assertion Types Demonstrated

- `content_includes` - Validates response contains specific patterns (case-insensitive)
- `content_matches` - Validates response matches regex pattern
- `tools_called` - Validates specific tools were called
- `tools_not_called` - Validates forbidden tools were NOT called

## Expected Behavior

- **scripted-turns**: Should pass if responses contain expected keywords (Paris, Python, programming)
- **self-play**: Should pass if assistant responses about renewable energy contain "energy" keyword
- **tool-usage**: Should pass if tools are called/not called as specified

## Self-Play with Assertions

The self-play scenario demonstrates assertions working with LLM-generated user messages:

```yaml
turns:
  # Turn 1: Initial scripted prompt with assertions on assistant response
  - role: user
    content: "Let's discuss renewable energy. Start by mentioning solar power."
    assertions:
      - type: content_includes
        params:
          patterns: ["renewable", "solar"]
  
  # Turn 2-3: Self-play continues (gemini-user generates messages)
  - role: gemini-user
    turns: 2
    assertions:
      - type: content_includes
        params:
          patterns: ["energy"]
```

Each assistant response (whether responding to a scripted or self-play user message) is validated against the assertions defined for that turn.
