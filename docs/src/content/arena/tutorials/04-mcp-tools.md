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

MCP servers are configured directly in your Arena configuration. The tools they provide are auto-discovered.

## Step 3: Configure Tools in Arena

Edit `arena.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: mcp-tools-test

spec:
  prompt_configs:
    - id: assistant
      file: prompts/assistant-with-tools.yaml
  
  providers:
    - file: providers/openai.yaml
  
  scenarios:
    - file: scenarios/tool-calling-test.yaml
  
  # Add MCP server configuration
  mcp_servers:
    memory:
      command: mcp-memory-server
      args: []
      env:
        LOG_LEVEL: info
```

## Step 4: Create Tool-Enabled Prompt

Create `prompts/assistant-with-tools.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: assistant-with-tools

spec:
  task_type: assistant
  
  system_template: |
    You are a helpful assistant with access to memory storage tools.
    
    When users ask you to remember information, use the store_memory tool.
    When users ask you to recall information, use the recall_memory tool.
    
    Always confirm when you've stored or retrieved information.
```

## Step 5: Create Tool-Calling Test

Create `scenarios/tool-calling-test.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-tool-calling
  labels:
    category: tools
    protocol: mcp

spec:
  task_type: assistant
  
  turns:
    # Turn 1: Request to store information
    - role: user
      content: "Remember that my favorite color is blue"
      assertions:
        - type: tools_called
          params:
            tools: ["store_memory"]
            message: "Should call store_memory tool"
        
        - type: content_includes
          params:
            text: "remember"
            message: "Should confirm storage"
    
    # Turn 2: Request to recall information
    - role: user
      content: "What's my favorite color?"
      assertions:
        - type: tools_called
          params:
            tools: ["recall_memory"]
            message: "Should call recall_memory tool"
        
        - type: content_includes
          params:
            text: "blue"
            message: "Should include recalled information"
```

## Step 6: Run Tool Tests

```bash
# Run with tools enabled
promptarena run --scenario tool-calling-test

# View detailed tool execution
promptarena run --verbose --scenario tool-calling-test
```

## Step 7: Mock Tool Responses

For testing without real tool execution, create mock tool definitions:

Create `tools/store-memory-mock.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: store-memory-mock

spec:
  name: store_memory
  description: "Store information in memory"
  
  input_schema:
    type: object
    properties:
      key:
        type: string
        description: "Memory key"
      value:
        type: string
        description: "Value to store"
    required: [key, value]
  
  mode: mock
  mock_result:
    success: true
    message: "Stored successfully"
```

Create `tools/recall-memory-mock.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: recall-memory-mock

spec:
  name: recall_memory
  description: "Recall stored information"
  
  input_schema:
    type: object
    properties:
      key:
        type: string
        description: "Memory key to recall"
    required: [key]
  
  mode: mock
  mock_template: |
    {
      "success": true,
      "value": "blue"
    }
```

Update `arena.yaml`:

```yaml
spec:
  # Use mock tools instead of MCP servers for testing
  tools:
    - file: tools/store-memory-mock.yaml
    - file: tools/recall-memory-mock.yaml
```

## Step 8: Complex Tool Scenarios

### Sequential Tool Calls

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: multiple-tool-operations
  labels:
    category: tools
    complexity: complex

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Remember: my name is Alice, email is alice@example.com, and I'm a developer"
      assertions:
        - type: tools_called
          params:
            tools: ["store_memory"]
            message: "Should call store_memory multiple times"
```

### Conditional Tool Use

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: conditional-tool-calling
  labels:
    category: conditional

spec:
  task_type: assistant
  
  turns:
    # Scenario where no tool is needed
    - role: user
      content: "What's 2+2?"
      assertions:
        - type: content_includes
          params:
            text: "4"
            message: "Should answer directly"
    
    # Scenario where tool is needed
    - role: user
      content: "Look up the weather in San Francisco"
      assertions:
        - type: tools_called
          params:
            tools: ["get_weather"]
            message: "Should call weather tool"
```

### Error Handling

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: tool-error-handling
  labels:
    category: error-handling

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Recall my favorite food"
      assertions:
        - type: tools_called
          params:
            tools: ["recall_memory"]
            message: "Should attempt to recall"
        
        - type: content_includes
          params:
            text: "don't have"
            message: "Should handle gracefully when not found"
```

## Step 9: Testing Different Tool Types

### Database Tools

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: database-query
  labels:
    category: database

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Find all users with role 'admin'"
      assertions:
        - type: tools_called
          params:
            tools: ["query_database"]
            message: "Should query database"
        
        - type: content_includes
          params:
            text: "admin"
            message: "Should mention admin users"
```

### API Integration

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: external-api-call
  labels:
    category: api

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Get the current Bitcoin price"
      assertions:
        - type: tools_called
          params:
            tools: ["fetch_crypto_price"]
            message: "Should call crypto API"
        
        - type: content_includes
          params:
            text: "Bitcoin"
            message: "Should mention Bitcoin"
```

### File Operations

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: file-read-operation
  labels:
    category: filesystem

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Read the contents of data.json"
      assertions:
        - type: tools_called
          params:
            tools: ["read_file"]
            message: "Should call read_file"
```

## Step 10: Advanced Tool Testing

### Tool Call Chains

Test when one tool call leads to another:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: tool-call-chain
  labels:
    category: chain

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Find Alice's email and send her a welcome message"
      assertions:
        - type: tools_called
          params:
            tools: ["lookup_user", "send_email"]
            message: "Should call both tools in sequence"
```

### Parallel Tool Calls

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: parallel-tool-execution
  labels:
    category: parallel

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Check the weather in New York, London, and Tokyo"
      assertions:
        - type: tools_called
          params:
            tools: ["get_weather"]
            message: "Should call weather tool for multiple locations"
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
assertions:
  - type: tools_called
    params:
      tools: ["correct_tool_name"]
      message: "Should call the right tool"
```

### 2. Validate Tool Calls

```yaml
# Check that tools are called appropriately
assertions:
  - type: tools_called
    params:
      tools: ["expected_tool"]
      message: "Should use the expected tool"
```

### 3. Mock External Dependencies

```yaml
# Use mock tools for external services
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: mock-external-api

spec:
  name: external_api
  description: "Mock external API"
  mode: mock
  mock_result:
    status: "success"
```

### 4. Test Error Scenarios

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: tool-failure-handling

spec:
  task_type: assistant
  
  turns:
    - role: user
      content: "Do something that requires a tool"
      assertions:
        - type: content_includes
          params:
            text: "error"
            message: "Should handle tool errors gracefully"
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
