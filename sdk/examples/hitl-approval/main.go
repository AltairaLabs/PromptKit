package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	fmt.Println("=== SDK HITL Approval Example ===")
	fmt.Println()

	ctx := context.Background()

	// Setup and create conversation
	conversation := setupConversation(ctx)

	// Send user message and get initial response
	response := sendUserMessage(ctx, conversation)

	// Handle approval workflow if needed
	if len(response.PendingTools) > 0 {
		handleApprovalWorkflow(ctx, conversation, response)
	} else {
		fmt.Println("\n✓ Request completed immediately (no approval needed)")
	}

	// Show final results
	showConversationHistory(conversation)
	showMethodsDemonstrated()
}

func setupConversation(ctx context.Context) *sdk.Conversation {
	fmt.Println("Step 1: Setting up conversation manager...")
	manager, pack, err := setupManager(ctx)
	if err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	fmt.Println("\nStep 2: Creating new conversation...")
	conversation, err := manager.CreateConversation(ctx, pack, sdk.ConversationConfig{
		UserID:     "user_123",
		PromptName: "support",
		Variables: map[string]interface{}{
			"user_name":    "Alice",
			"account_type": "premium",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create conversation: %v", err)
	}
	fmt.Printf("   Conversation ID: %s\n", conversation.GetID())

	return conversation
}

func sendUserMessage(ctx context.Context, conversation *sdk.Conversation) *sdk.Response {
	fmt.Println("\nStep 3: User requests refund...")
	userMessage := "I want a refund for my order #12345. The amount was $149.99."

	response, err := conversation.Send(ctx, userMessage)
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}

	fmt.Printf("   Bot response: %s\n", response.Content)
	fmt.Printf("   Pending tools: %d\n", len(response.PendingTools))

	return response
}

func handleApprovalWorkflow(ctx context.Context, conversation *sdk.Conversation, response *sdk.Response) {
	fmt.Println("\n⏸️  Tool execution requires approval")

	// Display pending tools
	displayPendingTools(response.PendingTools)

	// Simulate approval process
	simulateApprovalProcess()

	// Add approved tool result and continue
	addApprovedResultAndContinue(ctx, conversation)
}

func displayPendingTools(pendingTools []tools.PendingToolInfo) {
	for i, pending := range pendingTools {
		fmt.Printf("\n📋 Pending Tool #%d:\n", i+1)
		fmt.Printf("   Tool: %s\n", pending.ToolName)
		fmt.Printf("   Reason: %s\n", pending.Reason)
		fmt.Printf("   Message: %s\n", pending.Message)

		displayToolMetadata(pending.Metadata)
		displayToolDetails(pending)
	}
}

func displayToolMetadata(metadata map[string]interface{}) {
	if riskLevel, ok := metadata["risk_level"]; ok {
		fmt.Printf("   Risk Level: %s\n", riskLevel)
	}
	if amount, ok := metadata["amount"]; ok {
		fmt.Printf("   Amount: $%.2f\n", amount)
	}
	if orderID, ok := metadata["order_id"]; ok {
		fmt.Printf("   Order ID: %s\n", orderID)
	}
}

func displayToolDetails(pending tools.PendingToolInfo) {
	if pending.CallbackURL != "" {
		fmt.Printf("   Approval URL: %s\n", pending.CallbackURL)
	}
	if pending.ExpiresAt != nil && !pending.ExpiresAt.IsZero() {
		fmt.Printf("   Expires: %s\n", pending.ExpiresAt.Format("2006-01-02 15:04:05"))
	}
}

func simulateApprovalProcess() {
	fmt.Println("\n\nStep 5: Simulating supervisor approval...")
	fmt.Println("   [Notification sent to supervisor@example.com]")
	fmt.Println("   [Supervisor reviews refund request...]")
	time.Sleep(1 * time.Second) // Simulate review time
	fmt.Println("   [✓ Supervisor APPROVED the refund]")
}

func addApprovedResultAndContinue(ctx context.Context, conversation *sdk.Conversation) {
	fmt.Println("\nStep 6: Adding approved tool result...")

	toolCallID := findToolCallID(conversation)
	if toolCallID == "" {
		log.Fatal("Could not find tool call ID")
	}

	approvedResult := createApprovedResult()
	resultJSON, _ := json.Marshal(approvedResult)

	err := conversation.AddToolResult(toolCallID, string(resultJSON))
	if err != nil {
		log.Fatalf("Failed to add tool result: %v", err)
	}
	fmt.Println("   ✓ Tool result added")

	continueConversation(ctx, conversation)
}

func createApprovedResult() map[string]interface{} {
	return map[string]interface{}{
		"status":         "approved",
		"refund_amount":  149.99,
		"transaction_id": "refund_txn_789",
		"approved_by":    "supervisor@example.com",
		"approved_at":    time.Now().Format(time.RFC3339),
	}
}

func continueConversation(ctx context.Context, conversation *sdk.Conversation) {
	fmt.Println("\nStep 7: Resuming conversation...")
	finalResponse, err := conversation.Continue(ctx)
	if err != nil {
		log.Fatalf("Failed to continue: %v", err)
	}

	fmt.Println("\n✓ Conversation resumed successfully")
	fmt.Printf("   Final bot response: %s\n", finalResponse.Content)
	fmt.Printf("   Tokens used: %d\n", finalResponse.TokensUsed)
	fmt.Printf("   Latency: %dms\n", finalResponse.LatencyMs)
}

func showConversationHistory(conversation *sdk.Conversation) {
	fmt.Println("\n\nStep 8: Conversation History:")
	history := conversation.GetHistory()
	for i, msg := range history {
		fmt.Printf("   [%d] %s: %s\n", i+1, msg.Role, truncate(msg.Content, 60))
		if len(msg.ToolCalls) > 0 {
			fmt.Printf("       Tool calls: %d\n", len(msg.ToolCalls))
		}
		if msg.ToolResult != nil {
			fmt.Printf("       Tool result: %s\n", truncate(msg.ToolResult.Content, 60))
		}
	}
}

func showMethodsDemonstrated() {
	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey SDK Methods Demonstrated:")
	fmt.Println("✓ conversation.Send() - Send message and detect pending tools")
	fmt.Println("✓ response.PendingTools - Access pending tool information")
	fmt.Println("✓ conversation.AddToolResult() - Provide approved results")
	fmt.Println("✓ conversation.Continue() - Resume after approval")
	fmt.Println("✓ conversation.GetHistory() - View message history")
}

func setupManager(ctx context.Context) (*sdk.ConversationManager, *sdk.Pack, error) {
	// Create in-memory state store
	store := statestore.NewMemoryStore()

	// Create tool registry with async refund tool
	toolRegistry := tools.NewRegistry()

	// Register refund tool (uses mock-static executor)
	refundTool := NewAsyncRefundTool(true) // requireApproval = true
	toolRegistry.RegisterExecutor(refundTool)

	refundDescriptor := &tools.ToolDescriptor{
		Name:        "process_refund",
		Description: "Process a refund for a customer order. High-value refunds require supervisor approval.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"order_id": {"type": "string", "description": "Order ID to refund"},
				"amount": {"type": "number", "description": "Refund amount in dollars"},
				"reason": {"type": "string", "description": "Reason for refund"}
			},
			"required": ["order_id", "amount", "reason"]
		}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type": "string"},
				"refund_amount": {"type": "number"},
				"transaction_id": {"type": "string"}
			}
		}`),
		Mode:      "mock",
		TimeoutMs: 5000,
	}
	toolRegistry.Register(refundDescriptor)

	// Create mock provider
	provider := NewMockProvider()

	// Create conversation manager
	manager, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
		sdk.WithStateStore(store),
		sdk.WithToolRegistry(toolRegistry),
	)
	if err != nil {
		return nil, nil, err
	}

	// Create a simple pack for the support prompt
	pack := &sdk.Pack{
		Name:        "support",
		Version:     "1.0.0",
		Description: "Customer support pack",
		Prompts: map[string]*sdk.Prompt{
			"support": {
				Name:           "support",
				Description:    "Customer support assistant",
				SystemTemplate: "You are a helpful customer support assistant. You can process refunds using the process_refund tool. Always be polite and professional.",
				ToolNames:      []string{"process_refund"},
				Parameters: &sdk.Parameters{
					MaxTokens:   1000,
					Temperature: 0.7,
				},
				ToolPolicy: &sdk.ToolPolicy{
					ToolChoice:          "auto",
					MaxToolCallsPerTurn: 1,
				},
			},
		},
	}

	return manager, pack, nil
}

func findToolCallID(conversation *sdk.Conversation) string {
	history := conversation.GetHistory()

	// Look for the most recent assistant message with tool calls
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			return msg.ToolCalls[0].ID
		}
	}

	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// MockProvider simulates LLM responses for demonstration
type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) Name() string {
	return "mock"
}

func (m *MockProvider) ID() string {
	return "mock-provider-1"
}

func (m *MockProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Check if we have tool results - if so, give final response
	hasToolResult := false
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			hasToolResult = true
			break
		}
	}

	if hasToolResult {
		// Final response after tool execution
		return providers.PredictionResponse{
			Content: "Your refund has been processed successfully! You should see the refund in your account within 3-5 business days.",
		}, nil
	}

	// Initial response - call the refund tool
	return providers.PredictionResponse{
		Content: "I'll process that refund for you right away.",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_refund_123",
				Name: "process_refund",
				Args: json.RawMessage(`{"order_id":"12345","amount":149.99,"reason":"Customer requested refund"}`),
			},
		},
	}, nil
}

func (m *MockProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported")
}

func (m *MockProvider) SupportsStreaming() bool {
	return false
}

func (m *MockProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *MockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

func (m *MockProvider) Close() error {
	return nil
}
