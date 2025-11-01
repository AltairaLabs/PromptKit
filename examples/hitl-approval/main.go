package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func main() {
	fmt.Println("=== Human-in-the-Loop (HITL) Example ===")
	fmt.Println()
	fmt.Println("Scenario: Customer support with approval for high-value refunds")
	fmt.Println()
	fmt.Println("Note: This example demonstrates the AsyncToolExecutor interface and")
	fmt.Println("pending tool status. The full Continue() workflow requires additional")
	fmt.Println("middleware support (Phase 7 completion).")
	fmt.Println()

	// Setup
	approvalStore := newMockApprovalStore()
	toolRegistry := setupToolRegistry(approvalStore)
	mockProvider := setupMockProvider()

	// Create pipeline with provider middleware
	pipe := pipeline.NewPipeline(
		middleware.ProviderMiddleware(mockProvider, toolRegistry, nil, nil),
	)

	ctx := context.Background()

	// ========== TURN 1: User requests refund ==========
	fmt.Println("┌─────────────────────────────────────────────────┐")
	fmt.Println("│ TURN 1: Customer requests refund               │")
	fmt.Println("└─────────────────────────────────────────────────┘")
	fmt.Println()

	result1, err := pipe.Execute(ctx, "user", "I'd like to request a refund for my order #12345. The total was $450.")
	if err != nil {
		log.Fatalf("Turn 1 failed: %v", err)
	}

	printTurnResult("Assistant", result1, 1)

	// Check for pending tools
	hasPending := false
	for _, msg := range result1.Messages {
		if msg.Role == "tool" && msg.ToolResult != nil {
			// Pending tools have content (the pending message)
			if msg.ToolResult.Content != "" && msg.ToolResult.Error == "" {
				fmt.Println("\n⏸️  TOOL REQUIRES APPROVAL")
				fmt.Println()
				fmt.Printf("Tool: %s\n", msg.ToolResult.Name)
				fmt.Printf("Status: %s\n", msg.ToolResult.Content)
				hasPending = true
			}
		}
	}

	if !hasPending {
		log.Fatal("Expected pending tool in turn 1")
	}

	// Simulate time passing while waiting for approval
	fmt.Println("\n⏳ Waiting for supervisor approval...")
	fmt.Println("   [Email sent to supervisor@company.com]")
	fmt.Println("   [Supervisor reviews refund request...]")
	time.Sleep(500 * time.Millisecond)
	fmt.Println("   ✓ Supervisor APPROVED the refund")
	fmt.Println()

	// Approve the refund in the mock approval store
	approvalStore.approve("process_refund",
		json.RawMessage(`{"order_id":"12345","amount":450}`),
		json.RawMessage(`{"status":"approved","refund_id":"REF-789","processed_at":"2025-10-30T10:00:00Z","amount":450,"order_id":"12345"}`))

	fmt.Println("=== Example Complete ===")
	fmt.Println()
	fmt.Println("Key Takeaways:")
	fmt.Println("- AsyncToolExecutor interface enables HITL workflows")
	fmt.Println("- Tools return ToolExecutionResult with status (pending, complete, failed)")
	fmt.Println("- Pending tools include PendingInfo with reason, message, metadata")
	fmt.Println("- High-value refunds ($450) require approval via ExecuteAsync")
	fmt.Println("- Approval store pattern allows external systems to approve/reject")
	fmt.Println()
	fmt.Println("Next Steps:")
	fmt.Println("- Implement middleware support for HasPendingToolCalls() check")
	fmt.Println("- Add Continue() method to pipeline for resuming after approval")
	fmt.Println("- See docs/hitl-testing.md for architecture details")
	fmt.Println()
}

func setupToolRegistry(approvalStore *mockApprovalStore) *tools.Registry {
	registry := tools.NewRegistry()

	// Create async refund tool that requires approval for high-value refunds
	refundTool := NewAsyncRefundTool(approvalStore)

	// Register the executor
	registry.RegisterExecutor(refundTool)

	// Register tool descriptor - Mode will default to "mock-static"
	// and we've registered our AsyncRefundTool as the "mock-static" executor
	toolDescriptor := &tools.ToolDescriptor{
		Name:        "process_refund",
		Description: "Process a customer refund. Refunds over $100 require supervisor approval.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"order_id": {"type": "string", "description": "Order ID to refund"},
				"amount": {"type": "number", "description": "Refund amount in USD"}
			},
			"required": ["order_id", "amount"]
		}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type": "string"},
				"refund_id": {"type": "string"},
				"processed_at": {"type": "string"}
			}
		}`),
	}

	err := registry.Register(toolDescriptor)
	if err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	return registry
}

func setupMockProvider() *MockProvider {
	return NewMockProvider([]MockResponse{
		// Turn 1: User requests refund → Assistant calls tool
		{
			Content: "I'll process that refund for you right away.",
			ToolCalls: []types.MessageToolCall{
				{
					ID:   "call_refund_1",
					Name: "process_refund",
					Args: json.RawMessage(`{"order_id":"12345","amount":450}`),
				},
			},
		},
		// After tool returns pending, middleware currently makes another call
		// TODO: Fix middleware to check HasPendingToolCalls() and break loop
		{
			Content: "I've submitted your refund request for approval. A supervisor will review it shortly.",
		},
	})
}

func printTurnResult(speaker string, result *pipeline.ExecutionResult, turnNum int) {
	// Find the last assistant message
	var assistantMsg *types.Message
	for i := len(result.Messages) - 1; i >= 0; i-- {
		if result.Messages[i].Role == "assistant" {
			assistantMsg = &result.Messages[i]
			break
		}
	}

	if assistantMsg != nil {
		fmt.Printf("%s: %s\n", speaker, assistantMsg.Content)

		if len(assistantMsg.ToolCalls) > 0 {
			fmt.Printf("\nTool Calls (%d):\n", len(assistantMsg.ToolCalls))
			for _, tc := range assistantMsg.ToolCalls {
				fmt.Printf("   - %s\n", tc.Name)
				var args map[string]interface{}
				json.Unmarshal(tc.Args, &args)
				for k, v := range args {
					fmt.Printf("     %s: %v\n", k, v)
				}
			}
		}
	}

	// Show completed tools
	completedTools := 0
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolResult != nil && msg.ToolResult.Content != "" {
			completedTools++
		}
	}

	if completedTools > 0 {
		fmt.Printf("\n✓ %d tool(s) completed\n", completedTools)
	}

	fmt.Println()
}

func printPendingTools(messages []types.Message) {
	pendingCount := 0
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolResult != nil {
			// If Content is non-empty but there's no actual result, it's likely pending
			// (Pending tools get a message like "Tool X is pending approval")
			if msg.ToolResult.Content != "" && msg.ToolResult.Error == "" {
				pendingCount++
				fmt.Printf("📋 Pending Tool #%d:\n", pendingCount)
				fmt.Printf("   Tool: %s\n", msg.ToolResult.Name)
				fmt.Printf("   Message: %s\n", msg.ToolResult.Content)
			}
		}
	}
}

func countMessagesByRole(messages []types.Message, role string) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == role {
			count++
		}
	}
	return count
}

func countToolCalls(messages []types.Message) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == "assistant" {
			count += len(msg.ToolCalls)
		}
	}
	return count
}
