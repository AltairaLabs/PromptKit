---
title: Assertions Reference
---
Assertions are checks that verify LLM behavior during test execution. They run after each turn and determine whether the response meets expectations.

## How Assertions Work

```mermaid
sequenceDiagram
    participant Scenario
    participant LLM
    participant Assertion
    participant Result

    Scenario->>LLM: User Turn
    LLM-->>Scenario: Assistant Response

    loop For Each Assertion
        Scenario->>Assertion: Check Response
        Assertion->>Assertion: Evaluate
        Assertion-->>Result: Pass/Fail + Details
    end

    Result->>Scenario: Aggregate Results
```

## Assertion Structure

All assertions follow this structure:

```yaml
assertions:
  - type: assertion_name          # Required: Assertion type
    params:                       # Required: Type-specific parameters
      param1: value1
      param2: value2
    message: "Description"        # Optional: Human-readable description
```

**Fields**:
- `type`: The assertion type (see list below)
- `params`: Parameters specific to the assertion type
- `message`: Optional description shown in reports

## Available Assertions

### Content Assertions

#### `content_includes`

Checks that the response contains specific text patterns (case-insensitive).

**Use Cases**:
- Verify specific keywords appear
- Check that important information is mentioned
- Ensure acknowledgment phrases are present

**Parameters**:
- `patterns` (array of strings): Text patterns that must appear in response (all patterns must be found)

**Example**:
```yaml
- role: user
  content: "What is the capital of France?"
  assertions:
    - type: content_includes
      params:
        patterns: ["Paris"]
        message: "Should mention Paris"
```

**Matching**: Case-insensitive substring match
- ✅ "Paris" matches "The capital is Paris"
- ✅ "paris" matches "PARIS is the capital"
- ❌ "Pari" does not match "Paris"

**Multiple Patterns**:
```yaml
assertions:
  - type: content_includes
    params:
      patterns:
        - "Paris"
        - "France"
      message: "Should mention both Paris and France"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "missing_patterns": ["Paris"]
  }
}
```

---

#### `content_matches`

Checks that the response matches a regular expression pattern.

**Use Cases**:
- Flexible pattern matching
- Validate response structure
- Check for specific formats (emails, phone numbers, etc.)

**Parameters**:
- `pattern` (string): Regular expression pattern (Go regex syntax)

**Example**:
```yaml
- role: user
  content: "What's your email?"
  assertions:
    - type: content_matches
      params:
        pattern: "(?i)\\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Z|a-z]{2,}\\b"
        message: "Should provide email address"
```

**Pattern Examples**:

```yaml
# Case-insensitive word match
pattern: "(?i)hello"

# Match phone number
pattern: "\\d{3}-\\d{3}-\\d{4}"

# Match order number
pattern: "#\\d{5,}"

# Match "yes" or "no"
pattern: "(?i)\\b(yes|no)\\b"

# Match empathy phrases
pattern: "(?i)(understand|sorry|apologize|help)"
```

**Regex Flags**:
- `(?i)`: Case-insensitive
- `(?m)`: Multiline mode
- `(?s)`: Dot matches newline

**Common Patterns**:

```yaml
# Email
pattern: "(?i)\\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Z|a-z]{2,}\\b"

# URL
pattern: "https?://[^\\s]+"

# Order/Tracking Number
pattern: "#?[A-Z0-9]{6,}"

# Currency
pattern: "\\$\\d+(\\.\\d{2})?"

# Phone (US)
pattern: "\\(?\\d{3}\\)?[-.\\s]?\\d{3}[-.\\s]?\\d{4}"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "pattern": "(?i)email",
    "content": "Please call us instead"
  }
}
```

---

### Tool Call Assertions

#### `tools_called`

Verifies that specific tools were invoked in the response.

**Use Cases**:
- Verify function calling behavior
- Ensure LLM uses available tools
- Check tool selection logic

**Parameters**:
- `tools` (array): List of tool names that should be called

**Example**:
```yaml
- role: user
  content: "What's the weather in San Francisco?"
  assertions:
    - type: tools_called
      params:
        tools:
          - get_weather
        message: "Should call weather tool"
```

**Multiple Tools**:
```yaml
- role: user
  content: "Check my order and update my address"
  assertions:
    - type: tools_called
      params:
        tools:
          - check_order_status
          - update_customer_address
        message: "Should call both order and address tools"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "missing_tools": ["get_weather"],
    "called_tools": ["search_web"]
  }
}
```

**Important Notes**:
- Checks that ALL specified tools were called
- Order doesn't matter
- Tool may be called multiple times (counts as one)
- Only checks current turn, not previous turns

---

#### `tools_not_called`

Verifies that specific tools were NOT invoked in the response.

**Use Cases**:
- Ensure inappropriate tools aren't used
- Verify tool policies are enforced
- Check conditional tool usage

**Parameters**:
- `tools` (array): List of tool names that should NOT be called

**Example**:
```yaml
- role: user
  content: "What's the weather like?"
  assertions:
    - type: tools_not_called
      params:
        tools:
          - delete_account
          - charge_credit_card
        message: "Should not call destructive tools"
```

**Policy Enforcement**:
```yaml
# Ensure read-only operations
- role: user
  content: "Show me my account details"
  assertions:
    - type: tools_not_called
      params:
        tools:
          - update_account
          - delete_account
          - create_order
        message: "Should only read, not modify"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "forbidden_tools_called": ["delete_account"],
    "all_called_tools": ["get_account", "delete_account"]
  }
}
```

---

#### `tool_calls_with_args` (Turn-Level)

Verifies that a tool was called with specific arguments in the current turn. Supports both exact value matching and regex pattern matching.

**Use Cases**:
- Validate argument passing to tools
- Check parameter extraction from user input
- Ensure correct tool configuration
- Flexible pattern matching for dynamic values

**Parameters**:
- `tool_name` (string): Tool name to check
- `expected_args` (object): Exact argument values to match (use `null` for presence-only check)
- `args_match` (object): Regex patterns to match against argument values

**Example - Exact Match**:
```yaml
- role: user
  content: "What's the weather in San Francisco?"
  assertions:
    - type: tool_calls_with_args
      params:
        tool_name: get_weather
        expected_args:
          location: "San Francisco"
      message: "Should pass location correctly"
```

**Example - Regex Pattern Match**:
```yaml
- role: user
  content: "Analyze this image and describe it"
  assertions:
    - type: tool_calls_with_args
      params:
        tool_name: analyze_image
        args_match:
          description: "(?i)(logo|image|picture|graphic)"
      message: "Description should reference visual content"
```

**Example - Presence Check (any value)**:
```yaml
- role: user
  content: "Get weather for my city"
  assertions:
    - type: tool_calls_with_args
      params:
        tool_name: get_weather
        expected_args:
          location: null  # Just check that location exists
      message: "Should pass some location"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "violations": [
      {"type": "missing_argument", "tool": "get_weather", "argument": "location"},
      {"type": "pattern_mismatch", "tool": "analyze_image", "argument": "description", "pattern": "(?i)logo"}
    ]
  }
}
```

---

#### `tool_calls_with_args` (Conversation-Level)

Verifies that a tool was called with specific arguments across the entire conversation.

**Note**: This is a **conversation-level assertion**, used in the `conversation_assertions` field of a scenario, not in turn-level assertions.

**Use Cases**:
- Validate argument passing across conversation
- Check parameter extraction from user input
- Ensure correct tool configuration
- Pattern matching for dynamic argument values

**Parameters**:
- `tool_name` (string): Tool name to check
- `required_args` (object): Expected argument values (exact match)
- `args_match` (object): Regex patterns to match against argument values

**Example - Exact Match**:
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: tool-args-test
spec:
  task_type: test
  description: "Test tool arguments"
  turns:
    - role: user
      content: "What's the weather in San Francisco?"
  conversation_assertions:
    - type: tool_calls_with_args
      params:
        tool_name: get_weather
        required_args:
          location: "San Francisco"
      message: "Should pass location correctly"
```

**Example - Regex Pattern Match**:
```yaml
conversation_assertions:
  - type: tool_calls_with_args
    params:
      tool_name: search_products
      args_match:
        query: "(?i)(laptop|computer|electronics)"
    message: "Should search for electronics-related terms"
```

**Combined Exact and Pattern Match**:
```yaml
conversation_assertions:
  - type: tool_calls_with_args
    params:
      tool_name: create_order
      required_args:
        category: "electronics"
      args_match:
        product_id: "^PROD-[0-9]+$"
    message: "Should create order with valid product ID"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "tool": "get_weather",
    "expected": {"location": "San Francisco"},
    "actual": {"location": "SF"}
  }
}
```

---

### Agent Assertions

#### `agent_invoked` (Turn-Level)

Verifies that specific agents were invoked via tool calls in the current turn. Agent invocations appear as tool calls whose name matches the agent member name (e.g., `a2a__research_agent__search_papers`).

**Use Cases**:
- Verify multi-agent delegation behavior
- Check that the LLM routes requests to the correct agent
- Validate agent selection logic in orchestration scenarios

**Parameters**:
- `agents` (array of strings): Agent tool names that should have been called

**Example**:
```yaml
- role: user
  content: "Search for papers about quantum computing"
  assertions:
    - type: agent_invoked
      params:
        agents:
          - a2a__research_agent__search_papers
        message: "Should delegate to the research agent"
```

**Multiple Agents**:
```yaml
- role: user
  content: "Research quantum computing and translate the summary to French"
  assertions:
    - type: agent_invoked
      params:
        agents:
          - a2a__research_agent__search_papers
          - a2a__translation_agent__translate
        message: "Should delegate to both research and translation agents"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "missing_agents": ["a2a__research_agent__search_papers"]
  }
}
```

**Important Notes**:
- Checks that ALL specified agents were called
- Order doesn't matter
- Only checks the current turn, not previous turns
- For conversation-wide checks, use the conversation-level variant

---

#### `agent_not_invoked` (Turn-Level)

Verifies that specific agents were NOT invoked in the current turn.

**Use Cases**:
- Ensure the LLM handles requests directly without unnecessary delegation
- Verify agent routing policies are enforced
- Check that agents are only used when appropriate

**Parameters**:
- `agents` (array of strings): Agent tool names that should NOT have been called

**Example**:
```yaml
- role: user
  content: "What is 2 + 2?"
  assertions:
    - type: agent_not_invoked
      params:
        agents:
          - a2a__research_agent__search_papers
        message: "Should answer directly without delegating to research agent"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "forbidden_agents_called": ["a2a__research_agent__search_papers"]
  }
}
```

---

#### `agent_response_contains` (Turn-Level)

Verifies that a specific agent's response contains expected text. When an agent is invoked as a tool call, its response comes back as a tool result message. This assertion checks the content of that result.

**Use Cases**:
- Validate that a delegated agent returned the expected information
- Check that agent responses include specific keywords or data
- Verify end-to-end delegation pipeline output

**Parameters**:
- `agent` (string): The agent tool name whose response to check
- `contains` (string): Substring to look for in the agent's response

**Example**:
```yaml
- role: user
  content: "Search for papers about quantum computing"
  assertions:
    - type: agent_response_contains
      params:
        agent: a2a__research_agent__search_papers
        contains: "Quantum Computing Fundamentals"
        message: "Research agent should return quantum computing papers"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "agent": "a2a__research_agent__search_papers",
    "expected_substr": "Quantum Computing Fundamentals",
    "reason": "no matching agent response found containing expected text"
  }
}
```

---

#### `agent_invoked` (Conversation-Level)

Verifies that specific agents were invoked at least a minimum number of times across the entire conversation.

**Note**: This is a **conversation-level assertion**, used in the `conversation_assertions` field of a scenario, not in turn-level assertions.

**Use Cases**:
- Validate overall agent delegation patterns across a multi-turn conversation
- Ensure agents are invoked a minimum number of times
- Verify end-to-end orchestration behavior

**Parameters**:
- `agent_names` (array of strings): Required agent names
- `min_calls` (int, optional): Minimum number of calls per agent (default: 1)

**Example**:
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: multi-agent-test
spec:
  task_type: assistant
  description: "Test multi-agent delegation"
  turns:
    - role: user
      content: "Research quantum computing"
    - role: user
      content: "Translate the summary to French"
  conversation_assertions:
    - type: agent_invoked
      params:
        agent_names:
          - a2a__research_agent__search_papers
          - a2a__translation_agent__translate
        min_calls: 1
      message: "Both agents should be invoked at least once"
```

**Failure Details**:
```json
{
  "passed": false,
  "message": "missing required agent invocations: a2a__translation_agent__translate",
  "details": {
    "requirements": [
      {"agent": "a2a__research_agent__search_papers", "calls": 1, "requiredCalls": 1},
      {"agent": "a2a__translation_agent__translate", "calls": 0, "requiredCalls": 1}
    ],
    "counts": {
      "a2a__research_agent__search_papers": 1
    }
  }
}
```

---

#### `agent_not_invoked` (Conversation-Level)

Verifies that specific agents were NOT invoked anywhere in the conversation.

**Note**: This is a **conversation-level assertion**, used in the `conversation_assertions` field of a scenario, not in turn-level assertions.

**Use Cases**:
- Ensure forbidden agents are never invoked across the entire conversation
- Validate agent access control policies
- Verify that sensitive agents are not called in certain contexts

**Parameters**:
- `agent_names` (array of strings): Forbidden agent names

**Example**:
```yaml
conversation_assertions:
  - type: agent_not_invoked
    params:
      agent_names:
        - a2a__admin_agent__execute
    message: "Admin agent should never be invoked in user-facing scenarios"
```

**Failure Details**:
```json
{
  "passed": false,
  "message": "forbidden agents were invoked",
  "violations": [
    {
      "turn_index": 2,
      "description": "forbidden agent was invoked",
      "evidence": {
        "agent": "a2a__admin_agent__execute",
        "arguments": {"command": "..."}
      }
    }
  ]
}
```

---

### Workflow Assertions

#### `state_is`

Checks that the workflow is currently in a specific state. Used in workflow scenario steps to verify state machine position after transitions.

**Use Cases**:
- Verify the workflow reached an expected state after a transition
- Confirm the workflow hasn't progressed past a certain point
- Validate state machine routing logic

**Parameters**:
- `state` (string): The expected current state name

**Example**:
```yaml
steps:
  - type: input
    content: "I need help with billing"
    assertions:
      - type: state_is
        params:
          state: "intake"
        message: "Should still be in intake state"
```

**Failure Details**:
```json
{
  "passed": false,
  "message": "expected state \"intake\" but workflow is in state \"processing\"",
  "details": {
    "expected": "intake",
    "actual": "processing"
  }
}
```

---

#### `transitioned_to`

Checks that the workflow has transitioned to a specific state at any point during execution. Inspects the accumulated transition history.

**Use Cases**:
- Verify a specific state was visited during the workflow
- Confirm escalation or routing occurred
- Validate multi-step workflow progression

**Parameters**:
- `state` (string): The target state name that should appear in transition history

**Example**:
```yaml
steps:
  - type: input
    content: "I need to speak with a specialist about my billing issue"
    assertions:
      - type: transitioned_to
        params:
          state: "specialist"
        message: "LLM should have called workflow__transition to escalate"
```

**Failure Details**:
```json
{
  "passed": false,
  "message": "workflow never transitioned to state \"specialist\"",
  "details": {
    "expected_state": "specialist",
    "transitions": []
  }
}
```

---

#### `workflow_complete`

Checks that the workflow has reached a terminal state (a state with no outgoing transitions).

**Use Cases**:
- Verify the workflow completed successfully
- Confirm the conversation reached a natural end
- Validate end-to-end workflow execution

**Parameters**: None required.

**Example**:
```yaml
steps:
  - type: input
    content: "Thanks for your help, everything is resolved!"
    assertions:
      - type: workflow_complete
        message: "LLM should have resolved the workflow to a terminal state"
```

**Failure Details**:
```json
{
  "passed": false,
  "message": "workflow is not complete",
  "details": {
    "complete": false
  }
}
```

---

### Guardrail Assertions

#### `guardrail_triggered`

Verifies that a specific validator/guardrail was triggered.

**Use Cases**:
- Test that guardrails work correctly
- Verify policy enforcement
- Ensure validators catch violations

**Parameters**:
- `guardrail` (string): Validator type that should trigger
- `expected` (bool): Whether trigger is expected (default: true)

**Example**:
```yaml
# Verify banned word is caught
- role: user
  content: "Will this definitely work?"
  assertions:
    - type: guardrail_triggered
      params:
        guardrail: banned_words
        assertions: true
        message: "Should catch banned word 'definitely'"

# Verify no guardrail triggered
- role: user
  content: "This should be fine"
  assertions:
    - type: guardrail_triggered
      params:
        assertions: false
        message: "Should not trigger any guardrail"
```

**Testing Length Limits**:
```yaml
- role: user
  content: "Tell me everything about the universe"
  assertions:
    - type: guardrail_triggered
      params:
        guardrail: max_length
        assertions: true
        message: "Should trigger length limit"
```

**Failure Details**:
```json
{
  "passed": false,
  "details": {
    "expected_triggered": true,
    "actually_triggered": false,
    "guardrail": "banned_words"
  }
}
```

---

## Advanced Assertion Patterns

### Combining Assertions

Multiple assertions can check different aspects:

```yaml
- role: user
  content: "What's the weather?"
  assertions:
    # Check content
    - type: content_includes
      params:
        patterns: ["weather"]
        message: "Should acknowledge weather request"

    # Check tool usage
    - type: tools_called
      params:
        tools: ["get_weather"]
        message: "Should call weather tool"

    # Check response format
    - type: content_matches
      params:
        pattern: "\\d+°[CF]"
        message: "Should include temperature with unit"
```

### Conditional Logic

Use different assertions for different scenarios:

```yaml
# Test A: Expect tool call
- role: user
  content: "What's the weather in NYC?"
  assertions:
    - type: tools_called
      params:
        tools: ["get_weather"]

# Test B: Expect admission of limitation
- role: user
  content: "What's the weather next month?"
  assertions:
    - type: content_matches
      params:
        pattern: "(?i)(can't|cannot|unable|don't know)"
        message: "Should admit inability to predict future"
    - type: tools_not_called
      params:
        tools: ["get_weather"]
        message: "Should not attempt weather call for future"
```

### Progressive Verification

Check behavior across multiple turns:

```yaml
turns:
  # Turn 1: Initial request
  - role: user
    content: "I need help with my order"
    assertions:
      - type: content_includes
        params:
          patterns: ["order"]

  # Turn 2: Provide details
  - role: user
    content: "Order #12345"
    assertions:
      - type: content_includes
        params:
          patterns: ["#12345"]
      - type: tools_called
        params:
          tools: ["check_order_status"]

  # Turn 3: Follow-up
  - role: user
    content: "When will it arrive?"
    assertions:
      - type: content_matches
        params:
          pattern: "(?i)(date|time|day)"
```

## Assertion Best Practices

### 1. Be Specific

```yaml
# ❌ Too vague
- type: content_includes
  params:
    patterns: ["help"]

# ✅ Specific and meaningful
- type: content_includes
  params:
    patterns: ["Let me help you track your order"]
    message: "Should offer specific order tracking help"
```

### 2. Use Messages

```yaml
# ❌ No message
- type: content_matches
  params:
    pattern: "(?i)email"

# ✅ Clear message for reports
- type: content_matches
  params:
    pattern: "(?i)email"
    message: "Should provide email contact information"
```

### 3. Test Both Success and Failure

```yaml
# Test success case
- role: user
  content: "Get the weather"
  assertions:
    - type: tools_called
      params:
        tools: ["get_weather"]

# Test failure case (should NOT call tool)
- role: user
  content: "What's the weather like generally?"
  assertions:
    - type: tools_not_called
      params:
        tools: ["get_weather"]
        message: "Should not call tool for general question"
```

### 4. Use Appropriate Assertion Types

```yaml
# ❌ Using regex for simple text match
- type: content_matches
  params:
    pattern: "Paris"

# ✅ Using content_includes for simple text
- type: content_includes
  params:
    patterns: ["Paris"]

# ✅ Using regex for complex patterns
- type: content_matches
  params:
    pattern: "(?i)\\b(bonjour|hello|hi)\\b"
```

## Troubleshooting Assertions

### Assertion Always Fails

**Check**:
1. Case sensitivity (use `(?i)` in patterns)
2. Exact text vs substring matching
3. Whitespace and punctuation
4. Regex escaping

```yaml
# ❌ May fail due to case or punctuation
pattern: "Hello!"

# ✅ More robust
pattern: "(?i)hello"
```

### Tool Assertion Fails

**Check**:
1. Tool name matches exactly
2. Tool is registered in arena.yaml
3. Provider supports function calling
4. Prompt allows tool usage

### Regex Pattern Errors

**Common Issues**:

```yaml
# ❌ Missing escape
pattern: "."  # Matches any character

# ✅ Escaped literal dot
pattern: "\\."

# ❌ Unescaped dollar
pattern: "$100"

# ✅ Escaped dollar sign
pattern: "\\$100"
```

## Performance Considerations

### Assertion Execution

Assertions run synchronously after each turn:

```mermaid
graph LR
    Turn["User Turn"] --> LLM["LLM Response"]
    LLM --> A1["Assertion 1"]
    A1 --> A2["Assertion 2"]
    A2 --> A3["Assertion 3"]
    A3 --> Result["Turn Complete"]
```

**Tips**:
- Limit assertions per turn (3-5 recommended)
- Use specific patterns (avoid complex regex)
- Combine related checks when possible

### Regex Performance

```yaml
# ❌ Slow: backtracking
pattern: "(a+)+"

# ✅ Fast: specific
pattern: "\\ba+\\b"

# ❌ Slow: lookahead/lookbehind
pattern: "(?<=word)pattern(?=word)"

# ✅ Fast: simple match
pattern: "word.*pattern.*word"
```

## Next Steps

- **[Validators Reference](./validators-reference)** - Runtime guardrails
- **[Configuration Reference](./config-reference)** - Full config documentation
- **[Writing Scenarios](./writing-scenarios)** - Effective test authoring

---

**See Examples**: Check `examples/` directory for real-world assertion usage.
