---
layout: default
title: hitl-approval
parent: SDK Examples
grand_parent: Guides
---

# SDK HITL Approval Example

This example demonstrates how to use the PromptKit SDK to handle Human-in-the-Loop (HITL) tool approval workflows in your application.

## Overview

The SDK provides high-level APIs for detecting and managing pending tool approvals:

- `conversation.HasPendingTools()` - Check if conversation has pending approvals
- `conversation.GetPendingTools()` - Get detailed information about pending tools
- `conversation.AddToolResult()` - Provide approved tool results
- `conversation.Continue()` - Resume execution after approval

## Use Case

This example shows a customer support bot that requires approval before performing high-risk actions like:
- Issuing refunds
- Closing accounts
- Escalating to management

## Running the Example

```bash
cd sdk/examples/hitl-approval
go run .
```

## Expected Flow

1. User requests a refund
2. Bot recognizes this requires approval
3. Tool execution returns pending status
4. Application displays approval UI to supervisor
5. Supervisor approves the action
6. Application adds approved tool result
7. Conversation continues with final response

## Code Structure

- `main.go` - Complete HITL workflow demonstration
- `mock_provider.go` - Simulated LLM provider
- `async_refund_tool.go` - Example async tool with approval logic

## Key Concepts

### Detecting Pending Tools

```go
response, err := conversation.Send(ctx, "I want a refund for order #12345")
if err != nil {
    // Handle error
}

// Check if approval is needed
if len(response.PendingTools) > 0 {
    // Show approval UI
    for _, pending := range response.PendingTools {
        fmt.Printf("Approval needed: %s\n", pending.Message)
        fmt.Printf("Risk level: %s\n", pending.Metadata["risk_level"])
    }
}
```

### Adding Approved Results

```go
// After supervisor approval
approvedResult := `{"status":"approved","amount":149.99,"transaction_id":"txn_123"}`
err = conversation.AddToolResult(toolCallID, approvedResult)
if err != nil {
    // Handle error
}
```

### Resuming Execution

```go
// Continue conversation with approved result
finalResponse, err := conversation.Continue(ctx)
if err != nil {
    // Handle error
}

fmt.Printf("Final response: %s\n", finalResponse.Content)
```

## Integration with Real Systems

In a production application, you would:

1. **Persist conversation state** between approval and resumption
2. **Send notifications** to approvers (email, Slack, webhook)
3. **Track approval metadata** (who approved, when, reason)
4. **Implement timeout handling** for expired approvals
5. **Audit trail** for compliance

## See Also

- [SDK Documentation](../../README.md)
- [Runtime HITL Example](../../../examples/hitl-approval/README.md)
- [Tool Architecture](../../../docs/tool-architecture.md)
