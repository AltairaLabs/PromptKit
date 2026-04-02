---
title: 'Tutorial 8: Client-Side Tools'
---
Learn how to test tools that execute on the caller's device, including consent simulation for grant and deny scenarios.

## What You'll Learn

- Define client-mode tools that run on the user's device
- Use `mock_result` to provide deterministic test data
- Configure consent simulation for client-side tools
- Test both consent grant and consent deny scenarios

## Prerequisites

- Completed [Tutorials 1-3](/arena/tutorials/01-first-test/)
- Understanding of tool calling in LLMs (see [Tutorial 4](/arena/tutorials/04-mcp-tools/))

## What are Client-Side Tools?

Most tools execute server-side -- the LLM calls them, and the server runs the logic. **Client-side tools** are different: they execute on the caller's device. Think GPS location, camera access, biometric sensors, or local file pickers. The LLM requests the tool call, but the actual execution happens on the end user's client.

Because these tools access sensitive device capabilities, they typically require **user consent** before executing. PromptArena lets you simulate this consent flow so you can test how your LLM handles both granted and denied permissions.

## Step 1: Define a Client-Mode Tool

Create a tool definition file at `tools/get_location.tool.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_location
spec:
  description: "Get the user's current GPS coordinates"
  mode: client
  timeout_ms: 5000
  input_schema:
    type: object
    properties:
      accuracy:
        type: string
        enum: ["high", "low"]
        description: "Desired accuracy level"
  output_schema:
    type: object
    properties:
      lat:
        type: number
      lng:
        type: number
  mock_result:
    lat: 37.7749
    lng: -122.4194
  client:
    consent:
      required: true
      message: "Allow location access?"
      decline_strategy: error
    categories:
      - location
```

Key fields to note:

- **`mode: client`** -- marks this tool as client-side rather than server-executed.
- **`mock_result`** -- provides a fixed response when running in test mode. This is the data the LLM receives as if the device returned it.
- **`client.consent.required: true`** -- indicates the tool needs user permission before executing.
- **`client.consent.decline_strategy: error`** -- controls what happens when consent is denied. With `error`, the tool returns an error to the LLM so it can respond accordingly.

## Step 2: Create a Prompt Configuration

Create `prompts/chat.yaml` to tell the LLM about the available tool:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: chat
spec:
  task_type: "chat"
  version: "v1.0.0"
  description: "Chat assistant with client-side tool access"

  system_template: |
    You are a helpful assistant with access to the user's device capabilities.

    Available tools:
    - get_location: Get the user's GPS coordinates (requires user consent)

    When the user asks about their location, use the get_location tool.
    If the tool is unavailable or denied, politely inform the user that
    you are unable to access their location and suggest they provide it manually.

  allowed_tools:
    - get_location
```

## Step 3: Write Scenarios

You need two scenarios: one where the user grants consent and one where they deny it.

### Consent Grant Scenario

Create `scenarios/consent-grant.scenario.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: consent-grant
spec:
  id: consent-grant
  task_type: chat
  description: "Verifies that a client tool executes when consent is granted (default)"

  turns:
    - role: user
      content: "Where am I right now?"
      assertions:
        - type: tool_called
          params:
            tool_name: get_location
            message: "Should call get_location tool when user asks for location"

    - role: user
      content: "Tell me more about this area"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(san francisco|37\\.77|location)"
            message: "Should reference the location data from the tool result"
```

When no `consent_overrides` are specified, consent defaults to **grant**. The LLM calls `get_location`, receives the `mock_result` data, and uses it in its response.

### Consent Deny Scenario

Create `scenarios/consent-deny.scenario.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: consent-deny
spec:
  id: consent-deny
  task_type: chat
  description: "Verifies that the LLM handles consent denial gracefully"

  turns:
    - role: user
      content: "Where am I right now?"
      consent_overrides:
        get_location: deny
      assertions:
        - type: tool_called
          params:
            tool_name: get_location
            message: "Should attempt to call get_location tool"

    - role: user
      content: "Can you try again?"
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(unable|denied|provide|manually|sorry)"
            message: "Should explain that location access was denied and suggest alternatives"
```

The key line is `consent_overrides: { get_location: deny }`. This simulates the user declining the consent prompt. The LLM should detect the denial and respond gracefully rather than crashing or ignoring the error.

## Step 4: Configure Arena

Create `config.arena.yaml` to tie everything together:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: client-tools-consent
spec:
  prompt_configs:
    - id: chat
      file: prompts/chat.yaml

  providers:
    - file: providers/mock-provider.yaml

  scenarios:
    - file: scenarios/consent-grant.scenario.yaml
    - file: scenarios/consent-deny.scenario.yaml

  tools:
    - file: tools/get_location.tool.yaml

  defaults:
    temperature: 0.0
    max_tokens: 500
    seed: 42
    output:
      dir: out
      formats: ["json", "html"]
    fail_on:
      - provider_error
```

The `tools` section references the client-mode tool file. Arena registers the tool with the pipeline and handles consent simulation automatically based on your scenario overrides.

## Step 5: Run the Tests

Run the tests from your project directory:

```bash
PROMPTKIT_SCHEMA_SOURCE=local promptarena run --ci --format html
```

You should see output indicating both scenarios were executed:

- **consent-grant** -- the tool is called, `mock_result` is returned, and the LLM uses the coordinates in its response.
- **consent-deny** -- the tool call is attempted, consent is denied, and the LLM responds with a helpful fallback message.

Open `out/report.html` in a browser to view the full results.

## Summary

In this tutorial you learned how to:

1. Define a **client-mode tool** with `mode: client` that represents device-side functionality
2. Use **`mock_result`** to provide deterministic data during testing without a real device
3. Configure **consent simulation** with `client.consent` settings on the tool
4. Test the **consent grant** path (default behavior) to verify the tool executes correctly
5. Test the **consent deny** path using `consent_overrides` to verify graceful degradation

Testing consent flows is important because real users will sometimes deny permissions. Your LLM should always handle denial gracefully -- offering alternatives rather than failing silently.

For a complete working example, see the [`examples/client-tools/`](https://github.com/AltairaLabs/PromptKit/tree/main/examples/client-tools) directory in the PromptKit repository.

## Next Steps

- **[Tutorial 5: CI Integration](/arena/tutorials/05-ci-integration/)** -- automate client-tool tests in your CI/CD pipeline
- **[Tutorial 4: MCP Tools](/arena/tutorials/04-mcp-tools/)** -- learn about server-side tool testing for comparison
