---
title: 'Tutorial 5: Human-in-the-Loop'
sidebar:
  order: 5
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - sdk/conversation_tools.go
    - sdk/examples/hitl/hitl.pack.json
    - sdk/examples/hitl/main_interactive.go
    - sdk/response.go
    - sdk/tools/pending.go
---
In this tutorial we hold a tool call for human approval, then approve or reject it. We register the tool with `OnToolAsync()`, read the pending calls from a response, and send the outcome to the model with `Continue()`.

Human-in-the-loop (HITL) approval suits operations such as refunds, record deletion and outbound email, where a person should confirm before the tool runs.

## Prerequisites

Complete [Tutorial 3: Tools](/sdk/tutorials/03-tool-integration/).

## Step 1: Create the HITL pack

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
      "id": "refund_agent",
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

## Step 2: Register an async tool handler

We use `OnToolAsync()` for approval workflows:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
    "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
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
    resp, err := conv.Send(ctx, "Refund $150 for order #12345, damaged product")
    if err != nil {
        log.Fatal(err)
    }

    // Check for pending tools
    pending := resp.PendingTools()
    if len(pending) > 0 {
        for _, p := range pending {
            fmt.Printf("\nApproval required\n")
            fmt.Printf("Tool: %s\n", p.Name)
            fmt.Printf("Reason: %s\n", p.Reason)
            fmt.Printf("Message: %s\n", p.Message)
            fmt.Printf("Args: %v\n", p.Arguments)
            fmt.Print("\nApprove? (yes/no): ")

            input, _ := reader.ReadString('\n')
            input = strings.TrimSpace(strings.ToLower(input))

            if input == "yes" || input == "y" {
                result, err := conv.ResolveTool(ctx, p.ID)
                if err != nil {
                    log.Fatal(err)
                }
                fmt.Printf("Approved: %v\n", result.Result)
            } else {
                result, err := conv.RejectTool(ctx, p.ID, "Not authorized")
                if err != nil {
                    log.Fatal(err)
                }
                fmt.Printf("Rejected: %s\n", result.RejectionReason)
            }
        }

        // Send the outcomes to the model and read its final reply
        resp, err = conv.Continue(ctx)
        if err != nil {
            log.Fatal(err)
        }
    }

    fmt.Println("\nResponse:", resp.Text())
}
```

Run it. The $150 refund is over the $100 threshold, so the program prints `Approval required` with the tool name, the reason `high_value_refund` and the message `Refund of $150.00 requires approval`, then waits at the `Approve?` prompt. Answer `yes` and it prints `Approved:` with the refund result, then the model's final reply. Run it again and answer `no` to see `Rejected: Not authorized` before the model's final reply.

## Step 3: Read the check function

The check function returns a `tools.PendingResult`. A non-empty result holds the call for approval:

```go
func(args map[string]any) tools.PendingResult {
    // Check conditions
    if needsApproval(args) {
        return tools.PendingResult{
            Reason:  "approval_code",      // Machine-readable reason
            Message: "Human-readable msg", // Show to approver
        }
    }
    
    // An empty result runs the tool immediately
    return tools.PendingResult{}
}
```

A refund under $100 returns the empty result, so the tool runs at once and `PendingTools()` is empty.

## Step 4: Read the execute function

The execute function runs after approval, or immediately when the check function returns an empty result:

```go
func(args map[string]any) (any, error) {
    // Perform the actual operation
    return result, nil
}
```

Whatever this function returns is the tool result the model sees.

## Step 5: Handle pending tools

First we check a response for held calls:

```go
resp, err := conv.Send(ctx, message)
if err != nil {
    log.Fatal(err)
}

pending := resp.PendingTools()
if len(pending) > 0 {
    // Handle approvals
}
```

Then we approve a call:

```go
result, err := conv.ResolveTool(ctx, pendingID)
if err != nil {
    log.Printf("Resolve failed: %v", err)
}
fmt.Printf("Result: %v\n", result.Result)
```

This prints the map the execute function returned. After we resolve or reject every pending call, `Continue()` sends the results to the model and returns its reply:

```go
resp, err = conv.Continue(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Text())
```

The printed text is the model's reply to the outcomes. To reject a call instead:

```go
result, err := conv.RejectTool(ctx, pendingID, "Reason for rejection")
if err != nil {
    log.Printf("Reject failed: %v", err)
}
```

The rejected call is reported to the model when we call `Continue()`.

## Step 6: Combine approval conditions

A check function can hold a call for more than one reason:

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

A delete of `user` or `payment` records, or of more than 10 records, now returns a pending result with its own reason; any other delete runs at once.

## Step 7: Choose an approval interface

For a console, we prompt in a loop:

```go
for _, p := range resp.PendingTools() {
    fmt.Printf("Approve %s? [y/N]: ", p.Name)
    input, _ := reader.ReadString('\n')
    if strings.ToLower(strings.TrimSpace(input)) == "y" {
        conv.ResolveTool(ctx, p.ID)
    }
}
```

For a web service, an endpoint resolves the call:

```go
// API endpoint handles approval
http.HandleFunc("/approve", func(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Query().Get("id")
    if _, err := conv.ResolveTool(r.Context(), id); err != nil {
        http.Error(w, err.Error(), http.StatusConflict)
    }
})
```

A successful request returns status 200; if `ResolveTool` fails, the endpoint returns 409 with the error text.

## Next steps

- **[Tutorial 6: Observability](/sdk/tutorials/06-media-storage/)** - Monitor events with hooks
- **[How-To: Register Tools](/sdk/how-to/tools/register-tools/)** - Tool registration including async/HITL patterns
- The full example is at `sdk/examples/hitl/`.
