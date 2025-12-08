// Package main demonstrates Human-in-the-Loop (HITL) tool approval with the PromptKit SDK.
//
// This example shows:
//   - Using OnToolAsync for tools requiring approval
//   - Checking for pending tools in responses
//   - Resolving or rejecting pending tool calls
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
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
	// Open a conversation with HITL support
	conv, err := sdk.Open("./hitl.pack.json", "refund_agent")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Register a tool that requires approval for high-value refunds
	conv.OnToolAsync(
		"process_refund",
		// Check function - determines if approval is needed
		func(args map[string]any) tools.PendingResult {
			amount, _ := args["amount"].(float64)
			if amount > 100 {
				return tools.PendingResult{
					Reason:  "high_value_refund",
					Message: fmt.Sprintf("Refund of $%.2f requires human approval", amount),
				}
			}
			return tools.PendingResult{} // No approval needed
		},
		// Execution function - called after approval
		func(args map[string]any) (any, error) {
			orderID, _ := args["order_id"].(string)
			amount, _ := args["amount"].(float64)
			reason, _ := args["reason"].(string)

			// Simulate processing the refund
			return map[string]any{
				"status":       "completed",
				"order_id":     orderID,
				"refund_id":    "RF-" + orderID,
				"amount":       amount,
				"reason":       reason,
				"processed_at": "2025-01-15T14:30:00Z",
			}, nil
		},
	)

	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	// Start a conversation about a refund
	fmt.Println("=== Customer Support Agent ===")
	fmt.Println()

	resp, err := conv.Send(ctx, "I need a refund of $150 for order #12345 because the product was damaged")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	// Check if there are pending tools
	pending := resp.PendingTools()
	if len(pending) > 0 {
		fmt.Println("\n=== Pending Approval Required ===")
		for _, p := range pending {
			fmt.Printf("\nTool: %s\n", p.Name)
			fmt.Printf("Reason: %s\n", p.Reason)
			fmt.Printf("Message: %s\n", p.Message)
			fmt.Printf("Arguments: %v\n", p.Arguments)
			fmt.Print("\nApprove this action? (yes/no): ")

			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))

			if input == "yes" || input == "y" {
				// Approve and execute the tool
				result, err := conv.ResolveTool(p.ID)
				if err != nil {
					log.Printf("Failed to resolve tool: %v", err)
					continue
				}
				fmt.Printf("\nTool executed successfully:\n%v\n", result.Result)
			} else {
				// Reject the tool
				result, err := conv.RejectTool(p.ID, "Not authorized by supervisor")
				if err != nil {
					log.Printf("Failed to reject tool: %v", err)
					continue
				}
				fmt.Printf("\nTool rejected: %s\n", result.RejectionReason)
			}
		}

		// Continue the conversation after resolving pending tools
		resp, err = conv.Send(ctx, "Continue with the result of the tool call")
		if err != nil {
			log.Fatalf("Failed to continue: %v", err)
		}
	}

	fmt.Println("\n=== Agent Response ===")
	fmt.Println(resp.Text())
}
