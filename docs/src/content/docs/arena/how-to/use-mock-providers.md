---
title: Use Mock Providers
---
Learn how to use mock providers for fast, cost-free testing without calling real LLM APIs.

## Why Use Mock Providers?

- **Fast iteration**: Test configuration changes instantly
- **Zero cost**: No API charges during development
- **Deterministic**: Predictable responses for debugging
- **Offline testing**: Work without internet connection
- **CI/CD testing**: Fast pipeline validation without API dependencies

## Basic Usage

Replace all providers with mock:

```bash
# Use mock provider instead of real APIs
promptarena run --mock-provider
```

This replaces all configured providers with a mock that returns predefined responses.

## Mock Configuration File

Create custom mock responses with a configuration file:

### Simple Mock Config

```yaml
# mock-config.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: mock-responses

spec:
  type: mock
  
  responses:
    - pattern: "hello|hi|hey"
      response: "Hello! How can I help you today?"
    
    - pattern: "weather"
      response: "I can check the weather. What location are you interested in?"
    
    - pattern: ".*"  # Catch-all
      response: "I understand. Let me help with that."
```

### Run with Mock Config

```bash
promptarena run --mock-provider --mock-config mock-config.yaml
```

## Advanced Mock Patterns

### Regex Matching

```yaml
responses:
  # Match specific questions
  - pattern: "(what|how|why).*(hours|open|closed)"
    response: "Our business hours are Monday-Friday, 9 AM to 5 PM."
  
  # Match account-related queries
  - pattern: "account|billing|payment"
    response: "I can help with account and billing questions. What do you need?"
  
  # Match technical support
  - pattern: "(error|bug|crash|issue|problem)"
    response: "I'm sorry you're experiencing an issue. Let me help troubleshoot."
```

### Conditional Responses

```yaml
responses:
  # First turn response
  - pattern: ".*"
    turn: 1
    response: "Welcome! I'm here to assist you."
  
  # Follow-up responses
  - pattern: "thanks|thank you"
    response: "You're welcome! Anything else I can help with?"
  
  # Context-aware
  - pattern: "yes|sure|okay"
    context_required: "asked_for_confirmation"
    response: "Great! I'll proceed with that."
```

### Tool/Function Call Mocks

```yaml
responses:
  # Mock tool calling
  - pattern: "weather in (.*)"
    response: "Let me check the weather."
    tool_call:
      name: "get_weather"
      arguments:
        location: "$1"  # Captured from regex
      result:
        temperature: 72
        condition: "sunny"
```

### Multi-turn Scenarios

```yaml
responses:
  # Build conversation state
  - pattern: "I need help with my account"
    response: "I'd be happy to help. Can you provide your account ID?"
    set_context:
      support_type: "account"
  
  - pattern: "\\d{5}"  # Match 5-digit account ID
    context_required: "support_type=account"
    response: "Thank you. I've pulled up account $0. What can I help with?"
```

## Mock Strategies

### 1. Echo Mock (Development)

Simple echo for testing scenario structure:

```yaml
responses:
  - pattern: ".*"
    response: "Echo: $0"  # Returns user's message
```

### 2. Random Responses (Variation Testing)

Test assertion robustness:

```yaml
responses:
  - pattern: "greeting"
    responses:  # Array of possible responses
      - "Hello!"
      - "Hi there!"
      - "Greetings!"
      - "Hey!"
```

### 3. Failure Simulation

Test error handling:

```yaml
responses:
  - pattern: "trigger error"
    error:
      type: "rate_limit"
      message: "Rate limit exceeded"
      retry_after: 60
  
  - pattern: "timeout"
    delay: 30  # Simulate 30s delay
    response: "Delayed response"
```

### 4. Context-based Mocks

Simulate stateful conversations:

```yaml
responses:
  - pattern: "book flight to (.*)"
    response: "I can help book a flight to $1. What date?"
    set_context:
      destination: "$1"
      booking_state: "awaiting_date"
  
  - pattern: "(\\d{4}-\\d{2}-\\d{2})"
    context_required: "booking_state=awaiting_date"
    response: "Flight to ${context.destination} on $1. Confirm?"
    set_context:
      date: "$1"
      booking_state: "awaiting_confirmation"
```

## Provider Comparison Testing

Compare mock vs. real providers:

```yaml
# arena.yaml
providers:
  - path: ./providers/openai.yaml
  - path: ./providers/mock.yaml

scenarios:
  - path: ./scenarios/test.yaml
    providers: [openai-gpt4, mock]  # Test both
```

Run side-by-side:

```bash
# Compare real vs mock
promptarena run --scenario customer-support
```

## Development Workflow

### Phase 1: Build with Mocks

```bash
# Fast iteration with mock responses
promptarena run --mock-provider --mock-config dev-mocks.yaml
```

### Phase 2: Validate Structure

```bash
# Verify assertions work with mock data
promptarena run --mock-provider --format junit
```

### Phase 3: Real Provider Testing

```bash
# Test with actual providers
promptarena run --provider openai-gpt4
```

## CI/CD Integration

Use mocks for configuration validation:

```yaml
# .github/workflows/test.yml
name: Arena Tests

on: [push, pull_request]

jobs:
  mock-validation:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Validate Scenarios (Mock)
        run: |
          promptarena run --mock-provider --ci --format junit
      
      - name: Publish Results
        uses: dorny/test-reporter@v1
        with:
          name: Mock Tests
          path: out/junit.xml
          reporter: java-junit
  
  real-provider-tests:
    runs-on: ubuntu-latest
    needs: mock-validation  # Run after mock validation
    steps:
      - uses: actions/checkout@v3
      
      - name: Run Real Provider Tests
        env:
          OPENAI_API_KEY: $
        run: |
          promptarena run --provider openai-gpt4 --ci
```

## Complete Mock Example

```yaml
# mock-customer-support.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: mock-customer-support

spec:
  type: mock
  
  # Default behavior
  default_response: "I'm here to help. Can you provide more details?"
  
  # Specific patterns
  responses:
    # Greetings
    - pattern: "^(hello|hi|hey)"
      responses:
        - "Hello! How can I assist you today?"
        - "Hi there! What brings you here?"
    
    # Business hours
    - pattern: "(hours|open|closed|schedule)"
      response: "We're open Monday-Friday, 9 AM to 5 PM EST."
    
    # Billing
    - pattern: "(billing|payment|charge|invoice)"
      response: "I can help with billing questions. Do you have your account number?"
      set_context:
        topic: "billing"
    
    # Account lookup
    - pattern: "^[A-Z]{2}\\d{8}$"
      context_required: "topic=billing"
      response: "I found your account. Your current balance is $42.00."
    
    # Escalation
    - pattern: "(angry|frustrated|manager|escalate)"
      response: "I understand your frustration. Let me connect you with a supervisor."
      set_context:
        escalated: true
    
    # Tool calling
    - pattern: "weather in (.*)"
      response: "The current weather in $1 is sunny, 72°F."
      tool_call:
        name: "get_weather"
        arguments:
          location: "$1"
    
    # Error simulation
    - pattern: "trigger_error"
      error:
        type: "api_error"
        message: "Service temporarily unavailable"
```

Use this mock:

```bash
promptarena run --mock-provider --mock-config mock-customer-support.yaml
```

## Combining Mocks and Real Providers

Test specific scenarios with mocks while using real providers for others:

```yaml
# arena.yaml
scenarios:
  - path: ./scenarios/structure-validation.yaml
    providers: [mock]  # Fast validation
  
  - path: ./scenarios/quality-tests.yaml
    providers: [openai-gpt4, claude-sonnet]  # Real LLMs
```

## Next Steps

- **[Validate Outputs](validate-outputs)** - Add assertions to mock responses
- **[Integrate CI/CD](integrate-ci-cd)** - Use mocks in pipelines
- **[Tutorial: First Test](../tutorials/01-first-test)** - Complete walkthrough

## Examples

See complete mock configurations:
- `examples/mock-config-example.yaml` - Full mock setup
- `examples/customer-support/` - Mock vs real provider comparison
