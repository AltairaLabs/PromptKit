---
title: 'Tutorial 3: Multi-Turn Conversations'
sidebar:
  order: 3
---
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: account-issue-resolution
  labels:
    category: multi-turn
    type: customer-service

spec:
  task_type: support
  
  turns:
    # Turn 1: Initial problem statement
    - role: user
      content: "I can't access my account"
      assertions:
        - type: content_includes
          params:
            patterns: ["help"]
            message: "Should offer help"
    
    # Turn 2: Providing details
    - role: user
      content: "I get an error message saying 'Invalid credentials'"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(password|reset|credentials)"
            message: "Should reference password reset"
    
    # Turn 3: Follow-up question
    - role: user
      content: "How long will it take?"
      assertions:
        - type: content_includes
          params:
            patterns: ["time"]
            message: "Should provide timeframe"
    
    # Turn 4: Additional inquiry
    - role: user
      content: "Will I lose my saved preferences?"
      assertions:
        - type: content_includes
          params:
            patterns: ["preferences"]
            message: "Should address preferences concern"
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: flight-booking
  labels:
    category: progressive
    type: multi-turn

spec:
  task_type: support
  description: "Step-by-step information collection"
  
  context_metadata:
    session_goal: "Book a flight"
  
  turns:
    # Turn 1: Initial inquiry
    - role: user
      content: "I need to book a flight"
      assertions:
        - type: content_includes
          params:
            patterns: ["destination"]
            message: "Should ask for destination"
    
    # Turn 2: Provide destination
    - role: user
      content: "To New York"
      assertions:
        - type: content_includes
          params:
            patterns: ["date"]
            message: "Should ask for date"
    
    # Turn 3: Provide date
    - role: user
      content: "Next Friday"
      assertions:
        - type: content_includes
          params:
            patterns: ["class"]
            message: "Should ask for class preferences"
    
    # Turn 4: Complete booking
    - role: user
      content: "Economy class, window seat"
      assertions:
        - type: content_includes
          params:
            patterns: ["confirm"]
            message: "Should confirm booking details"
```

## Step 4: Conversation Branching

Test different conversation paths:

```yaml
# Path A: Successful resolution
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: happy-path-conversation
  labels:
    path: happy

spec:
  task_type: support
  
  turns:
    - role: user
      content: "My order hasn't arrived"
    - role: user
      content: "Order number is #12345"
    - role: user
      content: "Yes, the address is correct"
    - role: user
      content: "Great, thank you!"
      assertions:
        - type: content_includes
          params:
            patterns: ["welcome"]
            message: "Should acknowledge thanks positively"

---
# Path B: Escalation needed
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: escalation-path
  labels:
    path: escalation

spec:
  task_type: support
  
  turns:
    - role: user
      content: "My order hasn't arrived"
    - role: user
      content: "Order number is #12345"
    - role: user
      content: "No, I need it urgently"
    - role: user
      content: "This is unacceptable"
      assertions:
        - type: content_includes
          params:
            patterns: ["supervisor"]
            message: "Should offer escalation"
```

## Step 5: Testing Conversation Memory

Create `scenarios/memory-test.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: long-term-memory-test
  labels:
    category: memory
    type: context-retention

spec:
  task_type: support
  
  turns:
    # Turn 1: Introduction
    - role: user
      content: "Hi, my name is Alice and I'm calling about my account"
      assertions:
        - type: content_includes
          params:
            patterns: ["Alice"]
            message: "Should acknowledge name"
    
    # Turn 2-5: Other topics
    - role: user
      content: "What are your business hours?"
    - role: user
      content: "Do you offer international shipping?"
    - role: user
      content: "What's your return policy?"
    
    # Turn 6: Reference earlier context
    - role: user
      content: "What was my name again?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Alice"]
            message: "Should remember name from turn 1"
```

## Step 6: Conditional Responses

Test context-dependent responses:

```yaml
# Premium user scenario
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: premium-user-support

spec:
  task_type: support
  
  context_metadata:
    user_tier: premium
    account_id: "P-12345"
  
  turns:
    - role: user
      content: "I need help with my account"
      assertions:
        - type: content_includes
          params:
            patterns: ["premium"]
            message: "Should recognize premium tier"

---
# Basic user scenario
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-user-support

spec:
  task_type: support
  
  context_metadata:
    user_tier: basic
    account_id: "B-67890"
  
  turns:
    - role: user
      content: "I need help with my account"
      assertions:
        - type: content_includes
          params:
            patterns: ["help"]
            message: "Should offer helpful support"
```

## Step 7: Error Recovery

Test how the system handles conversation errors:

```yaml
# Clarification scenario
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: clarification-request
  labels:
    category: error-recovery

spec:
  task_type: support
  
  turns:
    - role: user
      content: "I need that thing"
      assertions:
        - type: content_includes
          params:
            patterns: ["clarify"]
            message: "Should ask for clarification"
    
    - role: user
      content: "Sorry, I meant the refund policy"
      assertions:
        - type: content_includes
          params:
            patterns: ["refund"]
            message: "Should proceed with clarified topic"

---
# Misunderstanding correction
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: misunderstanding-correction
  labels:
    category: correction

spec:
  task_type: support
  
  turns:
    - role: user
      content: "When can I get my order?"
    
    - role: user
      content: "Actually, I meant to ask about returns, not delivery"
      assertions:
        - type: content_includes
          params:
            patterns: ["return"]
            message: "Should pivot to the corrected topic"
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: self-play-customer-interaction
  labels:
    category: self-play

spec:
  task_type: support
  
  self_play:
    enabled: true
    persona: frustrated-customer
    max_turns: 10
    exit_conditions:
      - satisfaction_expressed
      - escalation_requested
```

Run self-play mode:

```bash
promptarena run --selfplay --scenario self-play-customer
```

### Conversation Patterns

#### Information Extraction

```yaml
spec:
  turns:
    - role: user
      content: "Book a table for 4 people tomorrow at 7pm"
      assertions:
        - type: content_includes
          params:
            patterns: ["4"]
            message: "Should capture party size"
```

#### Confirmation Loop

```yaml
spec:
  turns:
    - role: user
      content: "Cancel my subscription"
    
    - role: user
      content: "Yes, I'm sure"
      assertions:
        - type: content_includes
          params:
            patterns: ["confirm"]
            message: "Should confirm cancellation"
    
    - role: user
      content: "Can you tell me what I'll lose?"
      assertions:
        - type: content_includes
          params:
            patterns: ["lose"]
            message: "Should explain consequences"
```

## Best Practices

### 1. Test Realistic Conversation Flows

Model actual user interactions:

```yaml
# ✅ Good - natural conversation
spec:
  turns:
    - role: user
      content: "Hi, I have a question"
    - role: user
      content: "About shipping times"
    - role: user
      content: "To California"

# ❌ Avoid - too structured
spec:
  turns:
    - role: user
      content: "Question: What are shipping times to California?"
```

### 2. Validate Context at Each Turn

```yaml
spec:
  turns:
    - role: user
      content: "I'm having an issue"
    
    - role: user
      content: "With my recent order"
      assertions:
        - type: content_includes
          params:
            patterns: ["order"]
            message: "Should reference order context"
```

### 3. Test Edge Cases

```yaml
# Very long conversation
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: very-long-conversation

spec:
  task_type: support
  constraints:
    max_turns: 20
  turns:
    # ... 20+ turns

---
# Topic switching
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: topic-switching

spec:
  task_type: support
  turns:
    - role: user
      content: "Question about billing"
    - role: user
      content: "Actually, never mind, tell me about features"

---
# Ambiguous references
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: ambiguous-references

spec:
  task_type: support
  turns:
    - role: user
      content: "Tell me about plans"
    - role: user
      content: "What about that one?"
```

### 4. Use Context Metadata for Complex State

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: resume-conversation

spec:
  task_type: support
  
  context_metadata:
    previous_topic: "billing"
    unresolved_issues: ["payment failed"]
    user_mood: "frustrated"
  
  turns:
    - role: user
      content: "Let's continue where we left off"
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
assertions:
      patterns: ["I understand you mentioned your order number earlier."]

# ✅ Better
assertions:
  - type: content_includes
    params:
      patterns: ["order number"]
      message: "Should reference order"
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
