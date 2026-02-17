# SDK Workflow Example

This example demonstrates **LLM-initiated workflow transitions** using the PromptKit SDK.

## How It Works

The SDK's `WorkflowConversation` automatically injects a `workflow__transition` tool into the LLM's tool set for each non-terminal state. When the LLM decides a state change is needed (e.g., escalating a support ticket), it calls this tool with the target event and context summary. The SDK processes the transition after the response completes.

### Flow

1. `OpenWorkflow()` loads the pack, creates a state machine, and opens a conversation for the entry state
2. For each `Send()`, the LLM sees the current state's system prompt plus the `workflow__transition` tool
3. If the LLM calls the tool, the SDK transitions to the new state after the response
4. The loop continues until the workflow reaches a terminal state

## Running

```bash
# Set your API key
export OPENAI_API_KEY=sk-...
# Or: export ANTHROPIC_API_KEY=... / export GEMINI_API_KEY=...

# Run the example
cd examples/workflow-support/sdk-example
go run .
```

## Example Interaction

```
Starting state: intake

[intake] You: Hi, my order #12345 hasn't arrived
[intake] Agent: I'm sorry to hear that! Let me look into order #12345 for you...
[intake] You: It's been 2 weeks and tracking shows it's stuck
[specialist] Agent: I can see the shipment is delayed at the distribution center...
  → transitioned to specialist
[specialist] You: Can you send a replacement?
[closed] Agent: I've arranged a replacement shipment for order #12345...
  → transitioned to closed
  → workflow complete
```

## Key Concepts

- **`WithContextCarryForward()`** — passes a summary of the previous state's conversation to the new state via the `{{workflow_context}}` template variable
- **Orchestration modes** — `internal` (LLM decides), `external` (caller decides via `Transition()`), `hybrid` (both)
- **Terminal states** — states with no `on_event` transitions; the workflow completes when reached
