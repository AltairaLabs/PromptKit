---
title: 'Tutorial 4: Testing MCP Tools'
docType: tutorial
order: 4
---
# Tutorial 4: Testing MCP Tools

Learn how to test LLMs that use Model Context Protocol (MCP) tools and function calling.

## What You'll Learn

- Configure MCP tool servers
- Test tool/function calling
- Validate tool arguments
- Mock tool responses for testing
- Debug tool integration issues

## Prerequisites

- Completed [Tutorial 1-3](01-first-test)
- Understanding of function calling in LLMs
- Node.js installed (for MCP servers)

## What are MCP Tools?

Model Context Protocol (MCP) enables LLMs to interact with external systems:
- **Database queries**: Read/write data
- **API calls**: External service integration
- **File operations**: Read/write files
- **System commands**: Execute scripts

MCP standardizes how LLMs call tools across providers.

## Step 1: Install MCP Server

```bash
# Install the MCP filesystem server (example)
npm install -g @modelcontextprotocol/server-filesystem

# Or use PromptKit's built-in MCP memory server
cd $GOPATH/src/github.com/altairalabs/promptkit
go install ./runtime/mcp/servers/memory
```

## Step 2: Configure MCP Server

Create `mcp-servers.yaml`:

```yaml
version: "1.0"

servers:
  - name: memory
    command: mcp-memory-server
    args: []
    env:
      LOG_LEVEL: info
    
    tools:
      - name: store_memory
        description: "Store information in memory"
        parameters:
          type: object
          required: [key, value]
          properties:
            key:
              type: string
              description: "Memory key"
            value:
              type: string
              description: "Value to store"
      
      - name: recall_memory
        description: "Recall stored information"
        parameters:
          type: object
          required: [key]
          properties:
            key:
              type: string
              description: "Memory key to recall"
```

## Step 3: Configure Tools in Arena

Edit `arena.yaml`:

```yaml
version: "1.0"

prompts:
  - path: ./prompts

providers:
  - path: ./providers

scenarios:
  - path: ./scenarios

# Add MCP tools configuration
tools:
  mcp_servers:
    - path: ./mcp-servers.yaml
```

## Step 4: Create Tool-Enabled Prompt

Create `prompts/assistant-with-tools.yaml`:

```yaml
version: "1.0"
task_type: assistant

system_prompt: |
  You are a helpful assistant with access to memory storage tools.
  
  When users ask you to remember information, use the store_memory tool.
  When users ask you to recall information, use the recall_memory tool.
  
  Always confirm when you've stored or retrieved information.

user_prompt_template: |
  User: 

tools_enabled: true
```

## Step 5: Create Tool-Calling Test

Create `scenarios/tool-calling-test.yaml`:

```yaml
version: "1.0"
task_type: assistant

test_cases:
  - name: "Basic Tool Calling"
    tags: [tools, mcp]
    
    turns:
      # Turn 1: Request to store information
      - user: "Remember that my favorite color is blue"
        expected:
          # Verify tool was called
          - type: tool_called
            value: "store_memory"
          
          # Verify correct arguments
          - type: tool_args_match
            value:
              key: "favorite_color"
              value: "blue"
          
          # Verify confirmation response
          - type: contains
            value: ["remember", "stored", "saved"]
      
      # Turn 2: Request to recall information
      - user: "What's my favorite color?"
        expected:
          # Verify recall tool was called
          - type: tool_called
            value: "recall_memory"
          
          # Verify correct key
          - type: tool_args_match
            value:
              key: "favorite_color"
          
          # Verify correct information in response
          - type: contains
            value: "blue"
```

## Step 6: Run Tool Tests

```bash
# Run with tools enabled
promptarena run --scenario tool-calling-test

# View detailed tool execution
promptarena run --verbose --scenario tool-calling-test
```

## Step 7: Mock Tool Responses

For testing without real tool execution, use mock tools:

Create `tools/mock-tools.yaml`:

```yaml
version: "1.0"
mode: mock

mock_responses:
  store_memory:
    - request:
        key: "favorite_color"
        value: "blue"
      response:
        success: true
        message: "Stored successfully"
  
  recall_memory:
    - request:
        key: "favorite_color"
      response:
        success: true
        value: "blue"
    
    - request:
        key: "*"  # Catch-all
      response:
        success: false
        error: "Key not found"
```

Update `arena.yaml`:

```yaml
tools:
  mode: mock  # Use mock tools
  mock_config:
    - path: ./tools/mock-tools.yaml
  
  # Or use live tools
  # mode: live
  # mcp_servers:
  #   - path: ./mcp-servers.yaml
```

## Step 8: Complex Tool Scenarios

### Sequential Tool Calls

```yaml
test_cases:
  - name: "Multiple Tool Operations"
    tags: [tools, complex]
    
    turns:
      - user: "Remember: my name is Alice, email is alice@example.com, and I'm a developer"
        expected:
          # Should call store_memory multiple times
          - type: tool_called
            value: "store_memory"
            count: 3
          
          - type: tool_args_contain
            calls:
              - key: "name"
                value: "Alice"
              - key: "email"
                value: "alice@example.com"
              - key: "role"
                value: "developer"
```

### Conditional Tool Use

```yaml
test_cases:
  - name: "Conditional Tool Calling"
    tags: [conditional]
    
    turns:
      # Scenario where no tool is needed
      - user: "What's 2+2?"
        expected:
          - type: tool_called
            value: null  # No tool should be called
          - type: contains
            value: "4"
      
      # Scenario where tool is needed
      - user: "Look up the weather in San Francisco"
        expected:
          - type: tool_called
            value: "get_weather"
```

### Error Handling

```yaml
test_cases:
  - name: "Tool Error Handling"
    tags: [error-handling]
    
    turns:
      - user: "Recall my favorite food"
        mock_tool_response:
          recall_memory:
            success: false
            error: "Key not found"
        expected:
          - type: tool_called
            value: "recall_memory"
          
          - type: contains
            value: ["don't have", "haven't stored", "not found"]
          
          - type: tone
            value: helpful
```

## Step 9: Testing Different Tool Types

### Database Tools

```yaml
# scenarios/database-tools.yaml
version: "1.0"
task_type: assistant

test_cases:
  - name: "Database Query"
    tags: [database]
    
    turns:
      - user: "Find all users with role 'admin'"
        expected:
          - type: tool_called
            value: "query_database"
          
          - type: tool_args_match
            value:
              query: "SELECT * FROM users WHERE role = 'admin'"
          
          - type: contains
            value: ["found", "admin"]
```

### API Integration

```yaml
test_cases:
  - name: "External API Call"
    tags: [api]
    
    turns:
      - user: "Get the current Bitcoin price"
        expected:
          - type: tool_called
            value: "fetch_crypto_price"
          
          - type: tool_args_match
            value:
              symbol: "BTC"
          
          - type: contains
            value: ["Bitcoin", "price", "$"]
```

### File Operations

```yaml
test_cases:
  - name: "File Read Operation"
    tags: [filesystem]
    
    turns:
      - user: "Read the contents of data.json"
        expected:
          - type: tool_called
            value: "read_file"
          
          - type: tool_args_match
            value:
              path: "data.json"
```

## Step 10: Advanced Tool Testing

### Tool Call Chains

Test when one tool call leads to another:

```yaml
test_cases:
  - name: "Tool Call Chain"
    tags: [chain]
    
    turns:
      - user: "Find Alice's email and send her a welcome message"
        expected:
          # First tool call
          - type: tool_called
            value: "lookup_user"
            order: 1
          
          # Second tool call (uses result from first)
          - type: tool_called
            value: "send_email"
            order: 2
          
          - type: tool_args_match
            tool: "send_email"
            value:
              to: "alice@example.com"
```

### Parallel Tool Calls

```yaml
test_cases:
  - name: "Parallel Tool Execution"
    tags: [parallel]
    
    turns:
      - user: "Check the weather in New York, London, and Tokyo"
        expected:
          - type: tool_called
            value: "get_weather"
            count: 3
          
          - type: tool_args_contain
            parallel: true
            calls:
              - location: "New York"
              - location: "London"
              - location: "Tokyo"
```

## Debugging Tool Issues

### Check Tool Configuration

```bash
# Inspect tool configuration
promptarena config-inspect --verbose

# Should show loaded tools
```

### Verbose Tool Execution

```bash
# See detailed tool calls and responses
promptarena run --verbose --scenario tool-calling-test

# Output shows:
# [TOOL CALL] store_memory({"key": "favorite_color", "value": "blue"})
# [TOOL RESPONSE] {"success": true, "message": "Stored successfully"}
```

### Debug MCP Server

```bash
# Test MCP server directly
echo '{"method": "tools/list"}' | mcp-memory-server

# Check server logs
export LOG_LEVEL=debug
promptarena run --scenario tool-test
```

## Tool Testing Best Practices

### 1. Test Tool Selection

```yaml
# Verify correct tool is chosen
expected:
  - type: tool_called
    value: "correct_tool_name"
  
  - type: tool_not_called
    value: "wrong_tool_name"
```

### 2. Validate Arguments

```yaml
# Always check tool arguments
expected:
  - type: tool_args_match
    value:
      required_param: "expected_value"
  
  # Or use partial match
  - type: tool_args_contain
    value:
      key_field: "value"
```

### 3. Mock External Dependencies

```yaml
# Use mocks for external services
tools:
  mode: mock
  mock_config:
    - path: ./tools/mock-external-apis.yaml
```

### 4. Test Error Scenarios

```yaml
test_cases:
  - name: "Tool Failure Handling"
    turns:
      - user: "Do something"
        mock_tool_response:
          some_tool:
            error: "Service unavailable"
        expected:
          - type: error_handled
            value: true
```

## Common Issues

### Tool Not Called

```bash
# Check if tools are enabled in prompt
cat prompts/assistant-with-tools.yaml | grep tools_enabled

# Should be: tools_enabled: true
```

### Wrong Tool Arguments

```bash
# View actual tool calls
cat out/results.json | jq '.results[] | select(.tool_calls != null) | {
  tool: .tool_calls[].name,
  args: .tool_calls[].arguments
}'
```

### MCP Server Connection Failed

```bash
# Verify MCP server is running
ps aux | grep mcp

# Test MCP server directly
mcp-memory-server --help
```

## Next Steps

You now know how to test LLMs with tool calling!

**Continue learning:**
- **[Tutorial 5: CI Integration](05-ci-integration)** - Automate tool testing in CI/CD
- **[How-To: MCP Tools](../how-to/use-mock-providers)** - Advanced tool configuration
- **[Runtime: Tools & MCP](../../runtime/reference/tools-mcp)** - Complete tool reference

**Try this:**
- Create custom MCP tools
- Test tool calling across multiple providers
- Build a tool call chain test
- Mock complex external APIs

## What's Next?

In Tutorial 5, you'll learn how to integrate all these tests into your CI/CD pipeline for automated quality gates.
