---
title: 'Tutorial 3: Multi-Turn Conversations'
docType: tutorial
order: 3
---
# Tutorial 3: Multi-Turn Conversations

Learn how to test complex multi-turn conversations that maintain context across exchanges.

## What You'll Learn

- Create multi-turn conversation flows
- Test context retention across turns
- Handle conversation state
- Validate conversation coherence
- Test conversation branching

## Prerequisites

- Completed [Tutorial 1](01-first-test) and [Tutorial 2](02-multi-provider)
- Basic understanding of conversation design

## Why Multi-Turn Testing?

Real LLM applications involve conversations, not just single Q&A:
- **Customer support**: Back-and-forth troubleshooting
- **Chatbots**: Building rapport over multiple exchanges
- **Assistants**: Following complex instructions step-by-step
- **Agents**: Maintaining task state across turns

Multi-turn testing ensures:
- Context is retained between messages
- Responses reference previous exchanges
- Conversation flow feels natural
- State management works correctly

## Step 1: Basic Multi-Turn Scenario

Create `scenarios/support-conversation.yaml`:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Account Issue Resolution"
    tags: [multi-turn, customer-service]
    
    turns:
      # Turn 1: Initial problem statement
      - user: "I can't access my account"
        expected:
          - type: contains
            value: ["help", "account", "access"]
          - type: tone
            value: helpful
      
      # Turn 2: Providing details
      - user: "I get an error message saying 'Invalid credentials'"
        expected:
          # Should reference the previous context
          - type: references_previous
            value: true
          - type: contains
            value: ["password", "reset", "credentials"]
      
      # Turn 3: Follow-up question
      - user: "How long will it take?"
        expected:
          # Should understand "it" refers to password reset
          - type: references_previous
            value: true
          - type: contains
            value: ["time", "minutes", "hours"]
      
      # Turn 4: Additional inquiry
      - user: "Will I lose my saved preferences?"
        expected:
          # Should maintain conversation context
          - type: contains
            value: ["preferences", "saved", "keep"]
          - type: sentiment
            value: reassuring
```

## Step 2: Test Context Retention

Run the test:

```bash
promptarena run --scenario support-conversation
```

The `references_previous` assertion checks if the response demonstrates awareness of earlier turns.

## Step 3: Information Gathering Flow

Create `scenarios/progressive-disclosure.yaml`:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Step-by-Step Information Collection"
    tags: [progressive, multi-turn]
    
    turns:
      # Turn 1: Initial inquiry
      - user: "I need to book a flight"
        expected:
          - type: contains
            value: ["destination", "where", "travel"]
      
      # Turn 2: Provide destination
      - user: "To New York"
        context:
          destination: "New York"
        expected:
          - type: contains
            value: ["New York", "date", "when"]
          - type: references_previous
            value: true
      
      # Turn 3: Provide date
      - user: "Next Friday"
        context:
          destination: "New York"
          date: "next Friday"
        expected:
          - type: contains
            value: ["Friday", "class", "preferences"]
      
      # Turn 4: Complete booking
      - user: "Economy class, window seat"
        context:
          destination: "New York"
          date: "next Friday"
          class: "economy"
          seat: "window"
        expected:
          - type: contains
            value: ["economy", "window", "confirm", "New York"]
          # Should reference all collected information
          - type: references_previous
            value: true
```

## Step 4: Conversation Branching

Test different conversation paths:

```yaml
version: "1.0"
task_type: support

test_cases:
  # Path A: Successful resolution
  - name: "Happy Path Conversation"
    tags: [happy-path]
    
    turns:
      - user: "My order hasn't arrived"
      - user: "Order number is #12345"
      - user: "Yes, the address is correct"
      - user: "Great, thank you!"
        expected:
          - type: sentiment
            value: positive
  
  # Path B: Escalation needed
  - name: "Escalation Path"
    tags: [escalation]
    
    turns:
      - user: "My order hasn't arrived"
      - user: "Order number is #12345"
      - user: "No, I need it urgently"
      - user: "This is unacceptable"
        expected:
          - type: contains
            value: ["supervisor", "manager", "escalate"]
```

## Step 5: Testing Conversation Memory

Create `scenarios/memory-test.yaml`:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Long-Term Memory Test"
    tags: [memory, context]
    
    turns:
      # Turn 1: Introduction
      - user: "Hi, my name is Alice and I'm calling about my account"
        expected:
          - type: contains
            value: ["Alice", "account"]
      
      # Turn 2-5: Other topics
      - user: "What are your business hours?"
      - user: "Do you offer international shipping?"
      - user: "What's your return policy?"
      
      # Turn 6: Reference earlier context
      - user: "What was my name again?"
        expected:
          # Should remember name from turn 1
          - type: contains
            value: "Alice"
          - type: references_previous
            value: true
```

## Step 6: Conditional Responses

Test context-dependent responses:

```yaml
version: "1.0"
task_type: support

fixtures:
  premium_user:
    tier: "premium"
    account_id: "P-12345"
  
  basic_user:
    tier: "basic"
    account_id: "B-67890"

test_cases:
  - name: "Premium User Support"
    context:
      user: ${fixtures.premium_user}
    
    turns:
      - user: "I need help with my account"
        expected:
          - type: contains
            value: ["premium", "priority"]
          - type: tone
            value: personalized
  
  - name: "Basic User Support"
    context:
      user: ${fixtures.basic_user}
    
    turns:
      - user: "I need help with my account"
        expected:
          - type: tone
            value: helpful
```

## Step 7: Error Recovery

Test how the system handles conversation errors:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Clarification Request"
    tags: [error-recovery]
    
    turns:
      - user: "I need that thing"
        expected:
          # Should ask for clarification
          - type: contains
            value: ["clarify", "specific", "which"]
      
      - user: "Sorry, I meant the refund policy"
        expected:
          # Should proceed with clarified topic
          - type: contains
            value: ["refund", "policy"]
          - type: references_previous
            value: true
  
  - name: "Misunderstanding Correction"
    tags: [correction]
    
    turns:
      - user: "When can I get my order?"
      
      - user: "Actually, I meant to ask about returns, not delivery"
        expected:
          # Should pivot to the corrected topic
          - type: contains
            value: ["return", "policy"]
          - type: not_contains
            value: ["delivery", "shipping"]
```

## Step 8: Run Multi-Turn Tests

```bash
# Run all multi-turn tests
promptarena run --scenario support-conversation,progressive-disclosure,memory-test

# Generate detailed HTML report
promptarena run --format html

# View conversation flows
open out/report-*.html
```

## Analyzing Multi-Turn Results

### Review JSON Output

```bash
cat out/results.json | jq '.results[] | select(.scenario == "Account Issue Resolution") | {
  turn: .turn,
  user_message: .user_message,
  response: .response,
  assertions_passed: .assertions_passed
}'
```

### Check Context Retention

```bash
# Find tests with context retention issues
cat out/results.json | jq '.results[] | select(.assertions[] | 
  select(.type == "references_previous" and .passed == false))'
```

## Advanced Patterns

### Self-Play Testing

Test both sides of a conversation:

```yaml
version: "1.0"
task_type: support

test_cases:
  - name: "Self-Play Customer Interaction"
    tags: [self-play]
    
    self_play:
      enabled: true
      roles:
        - name: customer
          persona: "frustrated customer with billing issue"
        - name: agent
          persona: "helpful support agent"
    
    max_turns: 10
    
    success_criteria:
      - type: conversation_resolved
      - type: max_turns
        value: 10
```

Run self-play mode:

```bash
promptarena run --selfplay --scenario self-play-customer
```

### Conversation Patterns

#### Information Extraction

```yaml
turns:
  - user: "Book a table for 4 people tomorrow at 7pm"
    expected:
      - type: extracted_info
        fields:
          party_size: "4"
          date: "tomorrow"
          time: "7pm"
```

#### Confirmation Loop

```yaml
turns:
  - user: "Cancel my subscription"
  
  - user: "Yes, I'm sure"
    expected:
      - type: contains
        value: ["confirm", "cancelled"]
  
  - user: "Can you tell me what I'll lose?"
    expected:
      - type: references_previous
        value: true
```

## Best Practices

### 1. Test Realistic Conversation Flows

Model actual user interactions:

```yaml
# ✅ Good - natural conversation
turns:
  - user: "Hi, I have a question"
  - user: "About shipping times"
  - user: "To California"

# ❌ Avoid - too structured
turns:
  - user: "Question: What are shipping times to California?"
```

### 2. Validate Context at Each Turn

```yaml
turns:
  - user: "I'm having an issue"
  
  - user: "With my recent order"
    expected:
      - type: references_previous  # Always check
        value: true
```

### 3. Test Edge Cases

```yaml
test_cases:
  - name: "Very Long Conversation"
    turns:
      # ... 20+ turns
  
  - name: "Topic Switching"
    turns:
      - user: "Question about billing"
      - user: "Actually, never mind, tell me about features"
  
  - name: "Ambiguous References"
    turns:
      - user: "Tell me about plans"
      - user: "What about that one?"  # Ambiguous reference
```

### 4. Use Fixtures for Complex State

```yaml
fixtures:
  conversation_state_1:
    previous_topic: "billing"
    unresolved_issues: ["payment failed"]
    user_mood: "frustrated"

test_cases:
  - name: "Resume Conversation"
    context: ${fixtures.conversation_state_1}
```

## Common Issues

### Context Not Maintained

```bash
# Test with verbose logging
promptarena run --verbose --scenario memory-test

# Check if prompt includes conversation history
```

### Assertions Too Strict

```yaml
# ❌ Too strict
expected:
  - type: exact_match
    value: "I understand you mentioned your order number earlier."

# ✅ Better
expected:
  - type: references_previous
    value: true
  - type: contains
    value: ["order number", "mentioned"]
```

### Long Conversations Timeout

```bash
# Increase timeout for long conversations
promptarena run --timeout 300  # 5 minutes
```

## Next Steps

You now know how to test complex multi-turn conversations!

**Continue learning:**
- **[Tutorial 4: MCP Tools](04-mcp-tools)** - Test tool/function calling in conversations
- **[Tutorial 5: CI Integration](05-ci-integration)** - Automate conversation testing
- **[How-To: Write Scenarios](../how-to/write-scenarios)** - Advanced patterns

**Try this:**
- Create a 10+ turn conversation test
- Build a conversation decision tree
- Test conversation repair strategies
- Implement self-play testing

## What's Next?

In Tutorial 4, you'll learn how to test LLMs that use tools and function calling within conversations.
