---
title: Human-in-the-Loop (HITL) Example
description: Example demonstrating hitl
sidebar:
  order: 100
---


Approval workflows for sensitive operations with the PromptKit SDK.

## What You'll Learn

- Using `OnToolAsync()` for tools requiring approval
- Implementing approval check functions
- Handling pending tool calls
- Resolving or rejecting tools

## Prerequisites

- Go 1.21+
- OpenAI API key

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

```go
conv, err := sdk.Open("./hitl.pack.json", "refund_agent")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

// Register tool with approval check
conv.OnToolAsync(
    "process_refund",
    // Check function - determines if approval needed
    func(args map[string]any) tools.PendingResult {
        amount := args["amount"].(float64)
        if amount > 100 {
            return tools.PendingResult{
                Reason:  "high_value_refund",
                Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
            }
        }
        return tools.PendingResult{} // Auto-approve
    },
    // Execute function - runs after approval
    func(args map[string]any) (any, error) {
        orderID := args["order_id"].(string)
        return map[string]any{
            "status":   "processed",
            "order_id": orderID,
        }, nil
    },
)

// Send request - tool may require approval
resp, _ := conv.Send(ctx, "Refund $150 for order #12345")

// Check for pending approvals
for _, pending := range resp.PendingTools() {
    fmt.Printf("Pending: %s - %s\n", pending.Name, pending.Message)
    
    // Approve or reject
    if userApproves() {
        conv.ResolveTool(pending.ID)
    } else {
        conv.RejectTool(pending.ID, "Not authorized")
    }
}
```

## Pack File Structure

```json
{
  "prompts": {
    "refund_agent": {
      "system_template": "You are a customer support agent...",
      "tools": ["process_refund"]
    }
  },
  "tools": {
    "process_refund": {
      "name": "process_refund",
      "description": "Process a refund for a customer order",
      "parameters": {
        "type": "object",
        "properties": {
          "order_id": { "type": "string" },
          "amount": { "type": "number" },
          "reason": { "type": "string" }
        },
        "required": ["order_id", "amount", "reason"]
      }
    }
  }
}
```

## OnToolAsync Signature

```go
conv.OnToolAsync(
    name string,                              // Tool name
    check func(map[string]any) PendingResult, // Check if approval needed
    execute func(map[string]any) (any, error), // Execute after approval
)
```

### Check Function

Returns `PendingResult` to indicate if approval is needed:

```go
func(args map[string]any) tools.PendingResult {
    if needsApproval(args) {
        return tools.PendingResult{
            Reason:  "policy_violation",
            Message: "This action requires supervisor approval",
        }
    }
    return tools.PendingResult{} // Empty = auto-approve
}
```

## Approval Actions

```go
// Approve - executes the tool
result, err := conv.ResolveTool(pendingID)

// Reject - returns rejection to LLM
result, err := conv.RejectTool(pendingID, "Not authorized")
```

## Use Cases

- **Financial transactions** - Refunds over threshold
- **Data modifications** - Delete operations
- **External actions** - Send emails, API calls
- **Sensitive queries** - Access personal data

## Key Concepts

1. **Conditional Approval** - Check function decides
2. **Pending State** - Tools wait for human decision
3. **Async Workflow** - UI can handle approvals
4. **Audit Trail** - Log all approvals/rejections

## Next Steps

- [Hello Example](../hello/) - Basic conversation
- [Streaming Example](../streaming/) - Real-time responses
- [Tools Example](../tools/) - Basic function calling
