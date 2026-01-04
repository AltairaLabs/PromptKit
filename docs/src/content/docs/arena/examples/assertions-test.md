---
title: Assertions Test Example
description: Example demonstrating assertions-test
sidebar:
  order: 100
---
This example demonstrates turn-level assertions across four functional areas:

## Scenarios

1. **scripted-turns.yaml** - Scripted conversation turns with `content_includes` and `content_matches` assertions
2. **self-play.yaml** - Self-play mode with assertions on every turn  
3. **tool-usage.yaml** - Tool usage validation with `tools_called` and `tools_not_called` assertions
4. **json-validation.yaml** - JSON validation with `is_valid_json`, `json_schema`, and `json_path` assertions

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

# Run specific scenario with real API
./bin/promptarena run -c examples/assertions-test --scenario scripted-turns --provider gemini-flash
./bin/promptarena run -c examples/assertions-test --scenario tool-usage --provider openai-mini
./bin/promptarena run -c examples/assertions-test --scenario self-play --provider gemini-flash
./bin/promptarena run -c examples/assertions-test --scenario json-validation --provider openai-mini

# Run with mock provider (no API keys needed)
./bin/promptarena run -c examples/assertions-test --mock-provider --mock-config examples/assertions-test/mock-responses.yaml

# Run specific scenario with mock data
./bin/promptarena run -c examples/assertions-test --scenario json-validation --mock-provider --mock-config examples/assertions-test/mock-responses.yaml
```

## Assertion Types Demonstrated

### Content Assertions

- `content_includes` - Validates response contains specific patterns (case-insensitive)
- `content_matches` - Validates response matches regex pattern

### Tool Assertions

- `tools_called` - Validates specific tools were called
- `tools_not_called` - Validates forbidden tools were NOT called

### JSON Validation Assertions

- `is_valid_json` - Validates response contains parseable JSON (with optional extraction from markdown)
- `json_schema` - Validates JSON structure against JSON Schema specification
- `json_path` - Validates JSON content using JMESPath expressions with expected values and constraints
  - Uses `jmespath_expression` parameter (not JSON Path `$` syntax)
  - Backward compatible with `expression` parameter

## Expected Behavior

- **scripted-turns**: Should pass if responses contain expected keywords (Paris, Python, programming)
- **self-play**: Should pass if assistant responses about renewable energy contain "energy" keyword
- **tool-usage**: Should pass if tools are called/not called as specified
- **json-validation**: Should pass if responses contain valid JSON matching schemas and JSONPath queries

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

## JSON Validation with Mock and Real APIs

The json-validation scenario demonstrates comprehensive JSON validation capabilities that work with both mock responses and real API calls:

```yaml
turns:
  - role: user
    content: "Generate a user profile in JSON format..."
    assertions:
      # Validate JSON is parseable (extracts from markdown code blocks)
      - type: is_valid_json
        params:
          allow_wrapped: true
          extract_json: true
      
      # Validate against JSON Schema
      - type: json_schema
        params:
          schema:
            type: object
            required: ["name", "age", "email"]
            properties:
              name: {type: string}
              age: {type: integer, minimum: 18}
              email: {type: string, pattern: "^[a-zA-Z0-9._%+-]+@..."}
          allow_wrapped: true
          extract_json: true
      
      # Validate specific field values with JMESPath
      - type: json_path
        params:
          jmespath_expression: "name"
          expected: "Alice Smith"
          allow_wrapped: true
          extract_json: true
```

**Key Features:**

- **Automatic JSON Extraction**: Extracts JSON from markdown code blocks or mixed content
- **Schema Validation**: Validates structure, types, required fields, constraints
- **Field Validation**: Uses JMESPath expressions to validate specific values, array lengths, nested data
- **Mock Support**: Works with deterministic mock responses for rapid testing
- **Real API Support**: Validates actual LLM responses for production testing

**Test with mock data** (fast, no API keys):

```bash
./bin/promptarena run -c examples/assertions-test --scenario json-validation --mock-provider --mock-config examples/assertions-test/mock-responses.yaml
```

**Test with real API** (validates actual LLM output):

```bash
./bin/promptarena run -c examples/assertions-test --scenario json-validation --provider openai-mini
```
