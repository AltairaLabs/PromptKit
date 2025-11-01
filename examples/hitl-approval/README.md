# Human-in-the-Loop (HITL) Approval Example

This example demonstrates the Human-in-the-Loop (HITL) pattern using the `AsyncToolExecutor` interface. It shows how tools can return a "pending" status that requires external approval before completing execution.

## Scenario

A customer support chatbot that processes refunds. High-value refunds (over $100) require supervisor approval before processing, while low-value refunds execute immediately.

## Key Features

- **AsyncToolExecutor Interface**: Tools return `ToolExecutionResult` with status (complete/pending/failed)
- **Pending Status**: High-value operations return pending status with approval metadata
- **Approval Store Pattern**: Simulates external approval system integration
- **Risk-Based Logic**: Dynamic approval requirements based on operation value

## Architecture

```
┌─────────────┐
│   User      │
│  "Send email│
│  to john@..." │
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────────────────┐
│         Pipeline Execution                   │
│  ┌────────────────────────────────────────┐ │
│  │  LLM decides to use send_email tool    │ │
│  └────────────────────────────────────────┘ │
│                    ▼                         │
│  ┌────────────────────────────────────────┐ │
│  │  Provider Middleware                   │ │
│  │  - Detects AsyncToolExecutor           │ │
│  │  - Calls ExecuteAsync()                │ │
│  └────────────────────────────────────────┘ │
│                    ▼                         │
│  ┌────────────────────────────────────────┐ │
│  │  Email Tool Returns:                   │ │
│  │  Status: PENDING                       │ │
│  │  PendingInfo: {reason, message, ...}  │ │
│  └────────────────────────────────────────┘ │
│                    ▼                         │
│  ┌────────────────────────────────────────┐ │
│  │  ExecutionContext Updated:             │ │
│  │  - PendingToolCalls: [send_email]     │ │
│  │  - Metadata: pending_tools info        │ │
│  └────────────────────────────────────────┘ │
│                    ▼                         │
│  ┌────────────────────────────────────────┐ │
│  │  StateStore Middleware                 │ │
│  │  - Saves conversation state            │ │
│  │  - Saves pending tool calls            │ │
│  └────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│  Response to User:                   │
│  "Email requires approval.           │
│   Please review at: [approval link]" │
└──────────────────────────────────────┘
       │
       │ [Human reviews and approves]
       ▼
┌─────────────────────────────────────────────┐
│         Resumption Flow                      │
│  ┌────────────────────────────────────────┐ │
│  │  Load conversation from StateStore     │ │
│  └────────────────────────────────────────┘ │
│                    ▼                         │
│  ┌────────────────────────────────────────┐ │
│  │  Add tool result message:              │ │
│  │  {id: "call_123", content: "sent"}    │ │
│  └────────────────────────────────────────┘ │
│                    ▼                         │
│  ┌────────────────────────────────────────┐ │
│  │  Pipeline.Execute()                    │ │
│  │  - LLM sees tool result                │ │
│  │  - Generates final response            │ │
│  └────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│  Final Response:                     │
│  "I've sent the email to john@..."  │
└──────────────────────────────────────┘
```

## Files

- `main.go` - Main example program demonstrating the HITL flow
- `async_email_tool.go` - Custom tool that implements AsyncToolExecutor
- `approval_handler.go` - Simulates approval workflow
- `arena.yaml` - Arena configuration (if needed)

## Running the Example

```bash
cd examples/hitl-approval
go run .
```

## Key Concepts

### 1. AsyncToolExecutor Interface

Tools opt-in to async behavior by implementing `AsyncToolExecutor`:

```go
type AsyncToolExecutor interface {
    Executor
    ExecuteAsync(descriptor *ToolDescriptor, args json.RawMessage) (*ToolExecutionResult, error)
}
```

### 2. Tool Execution Status

Tools return a status indicating their state:

- `ToolStatusComplete` - Tool executed successfully
- `ToolStatusPending` - Tool needs external input (approval, API response, etc.)
- `ToolStatusFailed` - Tool execution failed

### 3. PendingToolInfo

When status is `Pending`, the tool provides metadata for middleware:

```go
type PendingToolInfo struct {
    Reason      string                 // "requires_approval"
    Message     string                 // "Email to CEO requires approval"
    ToolName    string                 // "send_email"
    Args        json.RawMessage        // Original arguments
    ExpiresAt   *time.Time            // Optional expiry
    CallbackURL string                 // Optional callback
    Metadata    map[string]interface{} // Additional data
}
```

### 4. ExecutionContext Integration

The pipeline automatically:
- Adds pending tools to `ExecutionContext.PendingToolCalls`
- Stores `PendingToolInfo` in `ExecutionContext.Metadata["pending_tools"]`
- Returns user-friendly message

### 5. State Persistence

StateStore middleware saves:
- Conversation messages
- Pending tool calls
- Metadata

### 6. Resumption

After approval:
1. Load conversation state
2. Add tool result message with approved result
3. Call `Pipeline.Execute()` again
4. LLM generates final response

## Example Output

```
Starting HITL Approval Example...

User: Send an email to ceo@example.com with subject "Q4 Report" and body "Please review the attached Q4 financial report."