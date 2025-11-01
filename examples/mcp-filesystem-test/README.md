# MCP Filesystem Integration Example

This example demonstrates an AI assistant with filesystem access capabilities using the MCP filesystem server.

## What This Example Shows

- **MCP Filesystem Integration**: Using the MCP filesystem server for file operations
- **File Operations**: Reading, writing, creating, moving files and directories
- **Multi-Turn Testing**: 10-turn scenario testing various filesystem operations
- **Real Tool Execution**: Actual filesystem tool calls (not mocks)
- **Working Directory**: All operations scoped to the `test_workspace` directory for safety

## Prerequisites

```bash
# Node.js/npm must be installed (for npx)
# No installation needed - npx will download the server automatically

# Test that node is available:
node --version
```

## Running the Example

**Important**: Run these commands from the project root directory, not from the example directory.

```bash
# Set your API key
export OPENAI_API_KEY="your-key-here"

# Run from the project root (you can now use directory path!)
./bin/promptarena run -c examples/mcp-filesystem-test --provider openai-gpt4o-mini --scenario file-operations

# Or inspect the configuration
./bin/promptarena config-inspect -c examples/mcp-filesystem-test --verbose

# Generate HTML report
./bin/promptarena run -c examples/mcp-filesystem-test --provider openai-gpt4o-mini --scenario file-operations --html
```

## Legacy Go Test

There's also a standalone Go test file (`test_filesystem.go`) that directly tests the MCP client library:

```bash
# Run the Go integration test
go run test_filesystem.go
```

## How It Works

### 1. MCP Server Configuration

The `arena.yaml` configures the MCP filesystem server with access to `test_workspace`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena

metadata:
  name: mcp-filesystem-test
  description: Test filesystem operations via MCP server

spec:
  mcp_servers:
    - name: filesystem
      command: npx
      args:
        - "-y"
        - "@modelcontextprotocol/server-filesystem"
        - "./test_workspace"  # Scoped to this directory only
      env:
        PATH: "/usr/local/bin:/usr/bin:/bin"  # Adjust if node is elsewhere
```

### 2. Prompt with Filesystem Instructions

The assistant prompt (`prompts/file-assistant.yaml`) instructs the LLM to use filesystem tools:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig

metadata:
  name: file-assistant

spec:
  task_type: file-assistant
  system_template: |
    You are a helpful AI assistant with filesystem access capabilities.
    
    You have access to the following filesystem tools:
    - read_file: Read the contents of a file
    - write_file: Write content to a file
    - list_directory: List files and directories
    - create_directory: Create a new directory
    - move_file: Move or rename files
    
    Always use tools when file operations are requested.
```

### 3. Test Scenario

The `file-operations` scenario tests 10 filesystem operations:

```yaml
# Turn 1-2: Create and write files
- role: user
  content: "Create a file called 'notes.txt' with content: 'Hello from PromptKit!'"
- role: user
  content: "Read the contents of notes.txt to verify it was created."

# Turn 3-4: Directory operations
- role: user
  content: "List all files in the test_workspace directory."
- role: user
  content: "Create another file called 'data.txt'"

# Turn 7-8: Advanced operations
- role: user
  content: "Create a subdirectory called 'archive'."
- role: user
  content: "Move data.txt into the archive subdirectory."

# And more...
```

### 4. Tool Policy

The scenario requires tool usage:

```yaml
tool_policy:
  tool_choice: required
  max_tool_calls_per_turn: 5
  max_total_tool_calls: 30
```

## File Structure

```text
mcp-filesystem-test/
├── README.md                      # This file
├── arena.yaml                     # Main configuration (K8s-style)
├── test_filesystem.go             # Legacy Go integration test
├── prompts/
│   └── file-assistant.yaml       # System prompt with filesystem instructions
├── scenarios/
│   └── file-operations.yaml      # Test scenario with 10 turns
├── providers/
│   └── openai-gpt4o-mini.yaml   # OpenAI GPT-4o Mini configuration
└── test_workspace/
    └── sample.txt                 # Sample file for testing
```

## Available MCP Tools

The filesystem server typically exposes:

- `read_file` - Read file contents
- `write_file` - Write to a file
- `list_directory` - List files in a directory
- `create_directory` - Create a new directory
- `move_file` - Move or rename files
- `search_files` - Search for files

## What Gets Tested

1. **File Creation**: Does the assistant create files with correct content?
2. **File Reading**: Does it read and report file contents accurately?
3. **Directory Listing**: Does it list directory contents correctly?
4. **Directory Creation**: Can it create subdirectories?
5. **File Moving**: Does it move files to new locations successfully?
6. **Path Resolution**: Are relative paths handled correctly?

## Troubleshooting

### Server Won't Start

Check if npx is available:

```bash
# Check if npx is available
which npx

# Try running the server manually
npx -y @modelcontextprotocol/server-filesystem ./test_workspace
```

### Permission Errors

Ensure the test workspace directory exists and is writable:

```bash
mkdir -p ./test_workspace
chmod 755 ./test_workspace
```

### PATH Issues

If you see "env: node: No such file or directory", update the PATH in `arena.yaml`:

```bash
# Find your node location
which node

# Update arena.yaml's env.PATH to include that directory
```
