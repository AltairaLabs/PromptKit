---
title: Test MCP Tools
description: Configure MCP servers in Arena scenarios for integration testing
sidebar:
  order: 12
---

Test MCP tool integrations in Arena by connecting real MCP servers and verifying tool calls in your scenarios.

---

## Configure MCP Servers

Add an `mcp_servers` block to your Arena config:

```yaml
# config.arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: mcp-test
spec:
  mcp_servers:
    - name: everything
      command: npx
      args:
        - "-y"
        - "@modelcontextprotocol/server-everything"

  prompt_configs:
    - id: assistant
      file: prompts/assistant.yaml
  scenarios:
    - file: scenarios/echo-test.yaml
  providers:
    - file: providers/mock-provider.yaml
```

Arena starts the MCP server, discovers its tools, and registers them for use in scenarios.

---

## Tool Filtering

MCP servers often expose many tools. Use `tool_filter` to limit which tools are available:

### Allowlist

```yaml
mcp_servers:
  - name: everything
    command: npx
    args: ["-y", "@modelcontextprotocol/server-everything"]
    tool_filter:
      allowlist:
        - echo
        - get-sum
```

### Blocklist

```yaml
mcp_servers:
  - name: database
    command: python
    args: ["mcp_db_server.py"]
    tool_filter:
      blocklist:
        - drop_table
        - truncate_table
```

---

## Server Configuration

### Environment Variables

Pass environment variables to the server process:

```yaml
mcp_servers:
  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${GITHUB_TOKEN}"
```

### Working Directory

```yaml
mcp_servers:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "./data"]
    working_dir: /path/to/project
```

### Timeout

Set a per-request timeout in milliseconds:

```yaml
mcp_servers:
  - name: everything
    command: npx
    args: ["-y", "@modelcontextprotocol/server-everything"]
    timeout_ms: 10000
```

---

## Prompt Config

Enable MCP tools in your prompt config with `allowed_tools`. MCP tools follow the naming pattern `mcp__{serverName}__{toolName}`:

```yaml
# prompts/assistant.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
spec:
  task_type: assistant
  version: v1.0.0
  description: Assistant with MCP tools
  allowed_tools:
    - mcp__everything__echo
    - mcp__everything__get-sum
  system_template: |
    You are a helpful assistant with access to echo and math tools.
```

---

## Mock LLM Responses

Configure the mock LLM to issue tool calls against MCP tools:

```yaml
# mock-responses.yaml
scenarios:
  echo-test:
    turns:
      1:
        tool_calls:
          - name: "mcp__everything__echo"
            arguments:
              message: "Hello from Arena!"
      2:
        response: "The echo tool returned: Hello from Arena!"
```

---

## Scenario with Assertions

```yaml
# scenarios/echo-test.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: echo-test
spec:
  id: echo-test
  task_type: assistant
  description: Test MCP echo tool
  tool_policy:
    tool_choice: auto
    max_tool_calls_per_turn: 3
  turns:
    - role: user
      content: "Echo the message: Hello from Arena!"
      assertions:
        - type: tools_called
          params:
            tools:
              - mcp__everything__echo
        - type: content_includes
          params:
            patterns:
              - "Hello from Arena"
```

---

## Multiple MCP Servers

Register multiple servers in the same config:

```yaml
mcp_servers:
  - name: everything
    command: npx
    args: ["-y", "@modelcontextprotocol/server-everything"]
    tool_filter:
      allowlist: [echo, get-sum]

  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "./data"]
    tool_filter:
      allowlist: [read_file, list_directory]
```

---

## Complete Example

```yaml
# config.arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: mcp-everything-test
spec:
  mcp_servers:
    - name: everything
      command: npx
      args: ["-y", "@modelcontextprotocol/server-everything"]
      timeout_ms: 10000
      tool_filter:
        allowlist: [echo, get-sum]

  prompt_configs:
    - id: mcp-assistant
      file: prompts/mcp-assistant.yaml
  scenarios:
    - file: scenarios/echo-and-add.scenario.yaml
  providers:
    - file: providers/mock-provider.yaml
  defaults:
    temperature: 0.7
    max_tokens: 500
    concurrency: 1
    output:
      dir: out
      formats: ["json", "html"]
```

Run it:

```bash
cd examples/mcp-everything-test
promptarena run -c config.arena.yaml
```

---

## Next Steps

- [Configure MCP Servers (SDK)](/sdk/how-to/configure-mcp/) — use MCP servers in the Go SDK
- [Integrate MCP (Runtime)](/runtime/how-to/integrate-mcp/) — low-level MCP registry API
- [Test A2A Agents](/arena/how-to/test-a2a-agents/) — test agent-to-agent delegation
- [Write Scenarios](/arena/how-to/write-scenarios/) — general scenario authoring
