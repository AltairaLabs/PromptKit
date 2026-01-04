---
title: 'Tutorial 5: Human-in-the-Loop'
sidebar:
  order: 5
---
Learn how to implement approval workflows for sensitive operations.

## What You'll Learn

- Implement tool approval with `OnToolAsync()`
- Check for pending approvals
- Approve or reject tool calls
- Build safe AI agents

## Why HITL?

Some operations should require human approval:

- **Financial transactions** - Refunds, purchases over threshold
- **Data modifications** - Delete records, update profiles
- **External actions** - Send emails, make API calls
- **Sensitive queries** - Access personal data

## Prerequisites

Complete [Tutorial 3: Tools](03-tool-integration) and understand tool registration.

## Step 1: Create HITL Pack

Create `hitl.pack.json`:

```json
{
  "id": "hitl-demo",
  "name": "HITL Demo",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "refund_agent": {
      "id": "refund-agent",
      "name": "Refund Agent",
      "version": "1.0.0",
      "system_template": "You are a customer support agent that processes refunds. For any refund request, use the process_refund tool.",
      "tools": ["process_refund"]
    }
  },
  "tools": {
    "process_refund": {
      "name": "process_refund",
      "description": "Process a customer refund",
      "parameters": {
        "type": "object",
        "properties": {
          "order_id": {
            "type": "string",
            "description": "Order ID to refund"
          },
          "amount": {
            "type": "number",
            "description": "Refund amount in dollars"
          },
          "reason": {
            "type": "string",
            "description": "Reason for refund"
          }
        },
        "required": ["order_id", "amount", "reason"]
      }
    }
  }
}
```

## Step 2: Register Async Tool Handler

Use `OnToolAsync()` for approval workflows:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/sdk/tools"
)

func main() {
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
            amount, _ := args["amount"].(float64)
            
            // High-value refunds need approval
            if amount > 100 {
                return tools.PendingResult{
                    Reason:  "high_value_refund",
                    Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
                }
            }
            
            // Auto-approve small refunds
            return tools.PendingResult{}
        },
        // Execute function - runs after approval
        func(args map[string]any) (any, error) {
            orderID, _ := args["order_id"].(string)
            amount, _ := args["amount"].(float64)
            reason, _ := args["reason"].(string)

            return map[string]any{
                "status":    "completed",
                "order_id":  orderID,
                "refund_id": "RF-" + orderID,
                "amount":    amount,
                "reason":    reason,
            }, nil
        },
    )

    ctx := context.Background()
    reader := bufio.NewReader(os.Stdin)

    // Start conversation
    resp, _ := conv.Send(ctx, "Refund $150 for order #12345, damaged product")

    // Check for pending tools
    pending := resp.PendingTools()
    if len(pending) > 0 {
        for _, p := range pending {
            fmt.Printf("\n⚠️  Approval Required\n")
            fmt.Printf("Tool: %s\n", p.Name)
            fmt.Printf("Reason: %s\n", p.Reason)
            fmt.Printf("Message: %s\n", p.Message)
            fmt.Printf("Args: %v\n", p.Arguments)
            fmt.Print("\nApprove? (yes/no): ")

            input, _ := reader.ReadString('\n')
            input = strings.TrimSpace(strings.ToLower(input))

            if input == "yes" || input == "y" {
                result, _ := conv.ResolveTool(p.ID)
                fmt.Printf("✅ Approved: %v\n", result.Result)
            } else {
                result, _ := conv.RejectTool(p.ID, "Not authorized")
                fmt.Printf("❌ Rejected: %s\n", result.RejectionReason)
            }
        }
    }

    fmt.Println("\nResponse:", resp.Text())
}
```

## Understanding OnToolAsync

### Check Function

Returns `PendingResult` to indicate if approval is needed:

```go
func(args map[string]any) tools.PendingResult {
    // Check conditions
    if needsApproval(args) {
        return tools.PendingResult{
            Reason:  "approval_code",      // Machine-readable reason
            Message: "Human-readable msg", // Show to approver
        }
    }
    
    // Empty result = auto-approve
    return tools.PendingResult{}
}
```

### Execute Function

Called after approval (or immediately if auto-approved):

```go
func(args map[string]any) (any, error) {
    // Perform the actual operation
    return result, nil
}
```

## Handling Pending Tools

### Check for Pending

```go
resp, _ := conv.Send(ctx, message)

pending := resp.PendingTools()
if len(pending) > 0 {
    // Handle approvals
}
```

### Approve a Tool

```go
result, err := conv.ResolveTool(pendingID)
if err != nil {
    log.Printf("Resolve failed: %v", err)
}
fmt.Printf("Result: %v\n", result.Result)
```

### Reject a Tool

```go
result, err := conv.RejectTool(pendingID, "Reason for rejection")
if err != nil {
    log.Printf("Reject failed: %v", err)
}
```

## Multiple Approval Conditions

Combine multiple checks:

```go
conv.OnToolAsync(
    "delete_record",
    func(args map[string]any) tools.PendingResult {
        recordType := args["type"].(string)
        count, _ := args["count"].(float64)
        
        // Check 1: Sensitive record types
        if recordType == "user" || recordType == "payment" {
            return tools.PendingResult{
                Reason:  "sensitive_data",
                Message: fmt.Sprintf("Deleting %s records requires approval", recordType),
            }
        }
        
        // Check 2: Bulk operations
        if count > 10 {
            return tools.PendingResult{
                Reason:  "bulk_operation",
                Message: fmt.Sprintf("Bulk delete of %d records requires approval", int(count)),
            }
        }
        
        return tools.PendingResult{}
    },
    func(args map[string]any) (any, error) {
        // Perform deletion
        return map[string]any{"deleted": true}, nil
    },
)
```

## Approval UI Patterns

### Console Approval

```go
fmt.Printf("Approve %s? [y/N]: ", pending.Name)
input, _ := reader.ReadString('\n')
if strings.ToLower(strings.TrimSpace(input)) == "y" {
    conv.ResolveTool(pending.ID)
}
```

### Web API Approval

```go
// Store pending for later approval
pendingStore[pending.ID] = pending

// API endpoint handles approval
http.HandleFunc("/approve", func(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Query().Get("id")
    conv.ResolveTool(id)
})
```

## What You've Learned

✅ Register async handlers with `OnToolAsync()`  
✅ Check for pending approvals  
✅ Approve with `ResolveTool()`  
✅ Reject with `RejectTool()`  
✅ Build safe approval workflows  

## Next Steps

- **[Tutorial 6: Media](06-media-storage)** - Working with images
- **[How-To: HITL Workflows](../how-to/hitl-workflows)** - Advanced patterns

## Complete Example

See the full example at `sdk/examples/hitl/`.
