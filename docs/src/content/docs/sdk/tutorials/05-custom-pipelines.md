---
title: 'Tutorial 5: Human-in-the-loop approval'
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
## What we will build

In this tutorial we build a refund agent that holds high-value refunds for a person to approve. We register the refund tool with `OnToolAsync()`, read the held calls from the response, approve or reject each one, and send the outcome to the model with `Continue()`.

## Prerequisites

Complete [Tutorial 3: Tools](/sdk/tutorials/03-tool-integration/), and set your provider API key as in Tutorial 1.

## Step 1: Create the pack

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

The pack declares one tool, `process_refund`, and a prompt that tells the model to use it.

## Step 2: Hold a refund and approve it

`OnToolAsync()` takes two functions. The check function returns a `tools.PendingResult`: a non-empty one holds the call for approval, and an empty one lets it run at once. The execute function runs once the call is approved, and its return value is the tool result the model sees.

Create `main.go`:

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
    message := "Refund $150 for order #12345, damaged product"
    if len(os.Args) > 1 {
        message = os.Args[1]
    }

    conv, err := sdk.Open("./hitl.pack.json", "refund_agent")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.OnToolAsync(
        "process_refund",
        // Check function: decides whether the call needs approval
        func(args map[string]any) tools.PendingResult {
            amount, _ := args["amount"].(float64)
            if amount > 100 {
                return tools.PendingResult{
                    Reason:  "high_value_refund",
                    Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
                }
            }
            return tools.PendingResult{}
        },
        // Execute function: runs after approval, or at once
        func(args map[string]any) (any, error) {
            orderID, _ := args["order_id"].(string)
            amount, _ := args["amount"].(float64)
            fmt.Printf("Executing refund of $%.2f for order %s\n", amount, orderID)
            return map[string]any{
                "status":    "completed",
                "refund_id": "RF-" + orderID,
                "amount":    amount,
            }, nil
        },
    )

    ctx := context.Background()
    reader := bufio.NewReader(os.Stdin)

    resp, err := conv.Send(ctx, message)
    if err != nil {
        log.Fatal(err)
    }

    pending := resp.PendingTools()
    for _, p := range pending {
        fmt.Println("Approval required")
        fmt.Println("Tool:", p.Name)
        fmt.Println("Reason:", p.Reason)
        fmt.Println("Message:", p.Message)
        fmt.Print("Approve? (yes/no): ")

        input, _ := reader.ReadString('\n')
        input = strings.TrimSpace(strings.ToLower(input))

        if input == "yes" || input == "y" {
            result, err := conv.ResolveTool(ctx, p.ID)
            if err != nil {
                log.Fatal(err)
            }
            fmt.Println("Approved:", result.Result)
        } else {
            result, err := conv.RejectTool(ctx, p.ID, "Not authorized")
            if err != nil {
                log.Fatal(err)
            }
            fmt.Println("Rejected:", result.RejectionReason)
        }
    }

    if len(pending) > 0 {
        // Send the outcomes to the model and read its final reply
        resp, err = conv.Continue(ctx)
        if err != nil {
            log.Fatal(err)
        }
    }

    fmt.Println("Response:", resp.Text())
}
```

Run it and answer `yes`:

```bash
go run main.go
```

The output depends on the model calling `process_refund`, which it typically does for this message. The $150 refund is over the $100 threshold, so `Send()` returns the call as pending instead of running it. `ResolveTool()` then runs the execute function, and `Continue()` returns the model's reply:

```text
Approval required
Tool: process_refund
Reason: high_value_refund
Message: Refund of $150.00 requires approval
Approve? (yes/no): yes
Executing refund of $150.00 for order 12345
Approved: map[amount:150 refund_id:RF-12345 status:completed]
Response: The refund of $150.00 for order 12345 has been processed.
```

The order ID and the final reply are the model's own wording, so they vary between runs. If the model answers without calling the tool, the program prints only the `Response:` line.

## Step 3: Reject the refund

Run the program again and answer `no`:

```bash
go run main.go
```

`RejectTool()` records the rejection without running the execute function, and `Continue()` reports it to the model:

```text
Approval required
Tool: process_refund
Reason: high_value_refund
Message: Refund of $150.00 requires approval
Approve? (yes/no): no
Rejected: Not authorized
Response: I'm sorry, I wasn't able to process that refund.
```

The reply typically tells the customer the refund did not go through.

## Step 4: Let a small refund run at once

Pass a refund under the threshold:

```bash
go run main.go 'Refund $50 for order #12346, wrong size'
```

The check function returns an empty `PendingResult`, so the tool runs inside `Send()` and nothing waits for approval:

```text
Executing refund of $50.00 for order 12346
Response: Your refund of $50.00 for order 12346 has been processed.
```

As in Step 2, the reply typically confirms the refund in the model's own words.

## Next steps

- [Tutorial 6: Observability](/sdk/tutorials/06-media-storage/): watch tool and provider events with hooks
- [Register Tools](/sdk/how-to/tools/register-tools/#async-tools-hitl): approve with edited arguments, resolve from an HTTP endpoint, and keep held calls across restarts
- The full example is in `sdk/examples/hitl/`.
