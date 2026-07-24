# External Workflow Orchestration Example

Drive workflow state transitions from outside the conversation loop.

## What it shows

In external orchestration mode, state transitions are triggered by outside callers
(HTTP handlers, message queues, etc.) rather than from within the conversation loop.
The `WorkflowConversation` is thread-safe for concurrent `Send()` and `Transition()`
calls from different goroutines.

## Running

```bash
cd sdk/examples/workflow-external
go run . -pack ./support.pack.json
```

Then interact over HTTP:

```bash
# Send a message to the current state's conversation
curl -X POST localhost:8080/send -d '{"message":"I need help with billing"}'

# Trigger a state transition
curl -X POST localhost:8080/transition -d '{"event":"Escalate"}'

# Check the current state
curl localhost:8080/state
```
