---
layout: default
title: mcp-memory-test
parent: PromptArena Examples
grand_parent: PromptArena
---


# MCP Memory Server Test

This example demonstrates PromptKit's MCP integration using the official `@modelcontextprotocol/server-memory` server.

## What This Tests

- **MCP Server Connection**: Validates that PromptKit can spawn and communicate with MCP servers via stdio
- **Tool Discovery**: Verifies that tools are discovered from the MCP server at runtime
- **Tool Execution**: Tests that LLM tool calls are routed to MCP server correctly
- **Memory Operations**: Demonstrates storing and retrieving data via MCP tools

## Prerequisites

1. **Node.js**: Required to run the MCP server via `npx`
2. **OpenAI API Key**: Set `OPENAI_API_KEY` environment variable
3. **PromptKit CLI**: Built from source (`make build`)

## Running the Test

```bash
# Set OpenAI API key
export OPENAI_API_KEY="your-api-key"

# Run the test
cd examples/mcp-memory-test
../../bin/promptarena run
```

## Expected Behavior

1. **Server Startup**: MCP memory server starts automatically
2. **Tool Discovery**: Tools discovered:
   - `store_memory(key: string, value: string)`
   - `retrieve_memory(key: string)`
3. **Turn 1**: User asks to remember favorite color
   - Assistant calls `store_memory(key="favorite_color", value="blue")`
   - Assistant confirms storage
4. **Turn 2**: User asks what favorite color is
   - Assistant calls `retrieve_memory(key="favorite_color")`
   - Assistant responds: "Your favorite color is blue"

## Debugging

### Enable Verbose Logging

```bash
# Set log level to debug
export LOG_LEVEL=debug
../../bin/promptarena run
```

### Check MCP Server Logs

MCP server output is logged to stderr. Look for:
- Initialization messages
- Tool list responses
- Tool execution requests/responses

### Manual Tool Discovery Test

You can test tool discovery separately:

```bash
# Run with debug flag to see discovered tools
../../bin/promptarena debug --config arena.yaml --list-tools
```

## Troubleshooting

### Error: "MCP server not found"

- Ensure Node.js is installed: `node --version`
- Test npx: `npx -y @modelcontextprotocol/server-memory --help`

### Error: "Tool not found: store_memory"

- Check tool discovery ran successfully
- Verify MCP server started (look for initialization logs)
- Check for timeout issues (increase timeout in code if needed)

### Error: "Connection refused" or "Process terminated"

- MCP server may have crashed
- Check server logs in stderr
- Try running server manually: `npx -y @modelcontextprotocol/server-memory`

## What's Being Tested

```
Configuration (arena.yaml)
    ↓
Engine.buildEngineComponents()
    ↓
buildMCPRegistry(cfg)
    └─→ Registers MCP server config
    
Engine initialization
    ↓
discoverAndRegisterMCPTools()
    ├─→ MCPRegistry.GetClient("memory")
    │    ├─→ Spawns: npx -y @modelcontextprotocol/server-memory
    │    ├─→ Initializes JSON-RPC connection
    │    └─→ Calls initialize() method
    │
    ├─→ client.ListTools()
    │    └─→ Returns: [store_memory, retrieve_memory]
    │
    └─→ Registers tools with mode="mcp"

Conversation Execution
    ↓
Turn 1: User asks to remember
    ↓
LLM decides to call store_memory
    ↓
ToolRegistry.Execute("store_memory", args)
    ├─→ Looks up tool: mode="mcp"
    ├─→ Routes to MCPExecutor
    └─→ MCPExecutor.Execute()
         ├─→ MCPRegistry.GetClientForTool("store_memory")
         ├─→ client.CallTool(name="store_memory", args={...})
         └─→ Returns result

Turn 2: User asks to recall
    ↓
LLM decides to call retrieve_memory
    ↓
[Same routing as above]
    └─→ Returns stored value
```

## Success Criteria

✅ MCP server starts without errors
✅ Tools are discovered and registered
✅ LLM successfully calls store_memory
✅ LLM successfully calls retrieve_memory
✅ Retrieved value matches stored value
✅ No connection errors or timeouts
✅ Server shuts down cleanly on exit

## Next Steps

After validating this test:
1. Add error handling for connection failures
2. Add retry logic for transient failures
3. Add timeout configuration
4. Create integration tests for edge cases
5. Test with multiple MCP servers simultaneously
