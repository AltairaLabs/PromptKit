package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// AsyncRefundTool implements AsyncToolExecutor for refund processing with approval
type AsyncRefundTool struct {
	approvalRequired bool
}

// RefundRequest represents refund parameters
type RefundRequest struct {
	OrderID string  `json:"order_id"`
	Amount  float64 `json:"amount"`
	Reason  string  `json:"reason"`
}

// NewAsyncRefundTool creates a new async refund tool
func NewAsyncRefundTool(requireApproval bool) *AsyncRefundTool {
	return &AsyncRefundTool{
		approvalRequired: requireApproval,
	}
}

// Name returns the executor name (using mock-static to integrate with registry)
func (t *AsyncRefundTool) Name() string {
	return "mock-static"
}

// Execute provides synchronous fallback
func (t *AsyncRefundTool) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return t.processRefundInternal(args)
}

// ExecuteAsync implements async tool execution with approval workflow
func (t *AsyncRefundTool) ExecuteAsync(descriptor *tools.ToolDescriptor, args json.RawMessage) (*tools.ToolExecutionResult, error) {
	var req RefundRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return &tools.ToolExecutionResult{
			Status: tools.ToolStatusFailed,
			Error:  fmt.Sprintf("invalid refund parameters: %v", err),
		}, nil
	}

	// Check if approval is required based on amount or reason
	if t.approvalRequired && t.requiresApproval(req) {
		expiresAt := time.Now().Add(24 * time.Hour)

		return &tools.ToolExecutionResult{
			Status: tools.ToolStatusPending,
			PendingInfo: &tools.PendingToolInfo{
				Reason:      "supervisor_approval_required",
				Message:     fmt.Sprintf("Refund of $%.2f requires supervisor approval", req.Amount),
				ToolName:    "process_refund",
				Args:        args,
				ExpiresAt:   &expiresAt,
				CallbackURL: fmt.Sprintf("http://localhost:8080/approve/refund_%d", time.Now().Unix()),
				Metadata: map[string]interface{}{
					"risk_level": t.assessRiskLevel(req),
					"amount":     req.Amount,
					"order_id":   req.OrderID,
					"reason":     req.Reason,
				},
			},
		}, nil
	}

	// No approval needed - process immediately
	result, err := t.processRefundInternal(args)
	if err != nil {
		return &tools.ToolExecutionResult{
			Status: tools.ToolStatusFailed,
			Error:  err.Error(),
		}, nil
	}

	return &tools.ToolExecutionResult{
		Status:  tools.ToolStatusComplete,
		Content: result,
	}, nil
}

// requiresApproval determines if refund needs approval
func (t *AsyncRefundTool) requiresApproval(req RefundRequest) bool {
	// Refunds over $100 require approval
	if req.Amount > 100.00 {
		return true
	}

	// Certain reasons require approval
	highRiskReasons := []string{"defective", "damaged", "fraud", "dispute"}
	lowerReason := strings.ToLower(req.Reason)
	for _, riskReason := range highRiskReasons {
		if strings.Contains(lowerReason, riskReason) {
			return true
		}
	}

	return false
}

// assessRiskLevel returns risk level for the refund
func (t *AsyncRefundTool) assessRiskLevel(req RefundRequest) string {
	if req.Amount > 500 {
		return "critical"
	}
	if req.Amount > 200 {
		return "high"
	}
	if req.Amount > 100 {
		return "medium"
	}
	return "low"
}

// processRefundInternal performs the actual refund processing
func (t *AsyncRefundTool) processRefundInternal(args json.RawMessage) (json.RawMessage, error) {
	var req RefundRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}

	fmt.Printf("💰 Processing refund:\n")
	fmt.Printf("   Order ID: %s\n", req.OrderID)
	fmt.Printf("   Amount: $%.2f\n", req.Amount)
	fmt.Printf("   Reason: %s\n", req.Reason)

	// Simulate refund processing
	result := map[string]interface{}{
		"status":         "completed",
		"refund_amount":  req.Amount,
		"order_id":       req.OrderID,
		"transaction_id": fmt.Sprintf("refund_txn_%d", time.Now().Unix()),
		"processed_at":   time.Now().Format(time.RFC3339),
	}

	return json.Marshal(result)
}

// ApproveAndExecute processes the refund after approval (for demonstration)
func (t *AsyncRefundTool) ApproveAndExecute(args json.RawMessage) (json.RawMessage, error) {
	fmt.Println("✅ Refund approved by supervisor")
	return t.processRefundInternal(args)
}
