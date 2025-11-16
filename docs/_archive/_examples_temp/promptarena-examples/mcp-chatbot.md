---
layout: default
title: mcp-chatbot
parent: PromptArena Examples
grand_parent: PromptArena
---


# MCP-Enabled Chatbot Example

This example demonstrates a conversational AI assistant with persistent memory using the Model Context Protocol (MCP).

## What This Example Shows

- **MCP Memory Integration**: Using the MCP knowledge graph server for persistent conversation memory
- **Multi-Turn Conversations**: Testing realistic multi-turn dialogues with 10 turns
- **Context Retention**: Verifying the assistant remembers previous interactions
- **Real Tool Execution**: Actual tool calls to MCP servers (not mocks)

## Prerequisites

```bash
# Node.js/npm must be installed (for npx)
# No installation needed - npx will download the server automatically

# Test that node is available:
node --version
```

## Running the Example

```bash
# Set your API key
export OPENAI_API_KEY="your-key-here"

# Run from the project root (you can now use directory path!)
./bin/promptarena run -c examples/mcp-chatbot --provider openai-gpt4o-mini --scenario memory-conversations

# Or inspect the configuration
./bin/promptarena config-inspect -c examples/mcp-chatbot --verbose

# Generate HTML report
./bin/promptarena run -c examples/mcp-chatbot --provider openai-gpt4o-mini --scenario memory-conversations --html
```

## How It Works

### 1. MCP Server Configuration

The `arena.yaml` configures the MCP memory server:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena

metadata:
  name: mcp-chatbot-example
  description: Conversational AI with persistent memory via MCP

spec:
  mcp_servers:
    - name: memory
      command: npx
      args:
        - "-y"
        - "@modelcontextprotocol/server-memory"
      env:
        PATH: "/usr/local/bin:/usr/bin:/bin"  # Adjust if node is elsewhere
```

### 2. Prompt with Memory Instructions

The assistant prompt (`prompts/memory-assistant.yaml`) instructs the LLM to use memory tools:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig

metadata:
  name: memory-assistant

spec:
  task_type: memory-assistant
  system_template: |
    You are a helpful AI assistant with memory capabilities using a knowledge graph.
    
    Use the following tools to remember information about the user:
    - create_entities: Store facts about people, preferences, skills, projects
    - create_relations: Link related concepts (e.g., person works_on project)
    - read_graph: Recall what you've previously learned
    
    When a user tells you something important, actively store it for future reference.
```

### 3. Test Scenario

The `memory-conversations` scenario tests 10 conversational turns:

```yaml
# Turn 1-2: Basic information storage
- role: user
  content: "Hi! My name is Alice and I'm a software engineer."
- role: user
  content: "What's my name and what do I do?"

# Turn 3-4: Adding preferences  
- role: user
  content: "I love Python programming and I'm currently working on a web scraper project."
- role: user
  content: "What programming language do I like?"

# Turn 5-6: Testing relationships
- role: user
  content: "I work with my colleague Dana on an AI project called PromptKit."
- role: user
  content: "Who do I work with and on what project?"

# And more...
```

### 4. Tool Policy

The scenario requires tool usage:

```yaml
tool_policy:
  tool_choice: required
  max_tool_calls_per_turn: 10
  max_total_tool_calls: 50
```

## Example Output

```text
Running Scenario: memory-conversations
Provider: openai-gpt4o-mini

Turn 1:
User: Hi! My name is Alice and I'm a software engineer.
Assistant: [calls create_entities]
Assistant: Nice to meet you, Alice! I've made a note that you're a software engineer.

Turn 2:
User: What's my name and what do I do?
Assistant: [calls read_graph]
Assistant: Your name is Alice, and you're a software engineer!

✅ Tool calls verified - Memory stored and retrieved correctly
```

## File Structure

```text
mcp-chatbot/
├── README.md                           # This file
├── arena.yaml                          # Main configuration (K8s-style)
├── prompts/
│   └── memory-assistant.yaml          # System prompt with memory instructions
├── scenarios/
│   └── memory-conversations.yaml      # Test scenario with 10 turns
└── providers/
    └── openai-gpt4o-mini.yaml        # OpenAI GPT-4o Mini configuration
```

## What Gets Tested

1. **Memory Storage**: Does the assistant store information when told?
2. **Memory Retrieval**: Does it recall stored information when asked?
3. **Context Understanding**: Does it know when to use memory vs. just respond?
4. **Tool Usage**: Are tool calls made with correct parameters?

## Validation Checks

The Arena validates:

- ✅ Tool calls are made appropriately (create_entities, read_graph)
- ✅ Responses are natural and conversational
- ✅ Information is accurately recalled
- ✅ No hallucination of facts

## Extending This Example

### Add More Memory Types

```yaml
# Store preferences
create_entities:
  - name: "Alice"
    type: "person"
    attributes:
      favorite_language: "Python"
      skill_level: "intermediate"

# Store project information
create_entities:
  - name: "WebApp Project"
    type: "project"
    attributes:
      tech_stack: ["Python", "FastAPI", "React"]
      status: "in-progress"

# Link them
create_relations:
  - from: "Alice"
    to: "WebApp Project"
    type: "working_on"
```

### Add Filesystem Access

Combine memory with filesystem for document-aware assistants:

```yaml
mcp_servers:
  - name: memory
    command: npx
    args: ["-y", "@modelcontextprotocol/server-memory"]
  
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "./docs"]
```

Then test scenarios like:

```yaml
- role: user
  content: "Read project-spec.md and remember the key requirements"
- role: assistant
  # Should call read_file, then create_entities to store requirements
```

## Troubleshooting

### Memory Not Persisting

The MCP memory server uses in-memory storage by default. Each test run starts fresh.

### Tool Calls Not Happening

Ensure your prompt explicitly instructs the LLM to use tools:

```yaml
system_template: |
  IMPORTANT: Use the memory tools actively.
  When users share information, call create_entities.
  When users ask what you know, call read_graph.
```

### Connection Errors

Check that npx can access the MCP server:

```bash
# Test manually
npx -y @modelcontextprotocol/server-memory

# Should see: "Knowledge Graph MCP Server running on stdio"
```

### PATH Issues

If you see "env: node: No such file or directory", update the PATH in `arena.yaml`:

```bash
# Find your node location
which node

# Update arena.yaml's env.PATH to include that directory
```

## Learn More

- [MCP Integration Guide](../../docs/mcp-integration.md) - Full MCP documentation
- [Tool Calling Guide](../../docs/tool-calling.md) - How tool execution works
- [Official MCP Servers](https://github.com/modelcontextprotocol/servers) - More MCP servers to try
