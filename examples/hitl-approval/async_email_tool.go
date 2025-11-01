package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// AsyncRefundTool implements AsyncToolExecutor for processing refunds with approval
type AsyncRefundTool struct {
	approvalStore *mockApprovalStore
}

// RefundRequest represents the parameters for processing a refund
type RefundRequest struct {
	OrderID string  `json:"order_id"`
	Amount  float64 `json:"amount"`
}

// NewAsyncRefundTool creates a new async refund tool
func NewAsyncRefundTool(approvalStore *mockApprovalStore) *AsyncRefundTool {
	return &AsyncRefundTool{
		approvalStore: approvalStore,
	}
}

// Name returns the executor name - using "mock-static" to override default
func (t *AsyncRefundTool) Name() string {
	return "mock-static"
}

// Execute provides synchronous fallback
func (t *AsyncRefundTool) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	// Fallback to sync execution
	var req RefundRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	return t.processRefundInternal(req)
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

	// Check if approval already exists
	if approval := t.approvalStore.get(args); approval != nil {
		if approval.rejected {
			return &tools.ToolExecutionResult{
				Status: tools.ToolStatusFailed,
				Error:  approval.reason,
			}, nil
		}
		// Approved - execute
		return &tools.ToolExecutionResult{
			Status:  tools.ToolStatusComplete,
			Content: approval.result,
		}, nil
	}

	// Check if approval is required
	if req.Amount > 100 {
		// High-value refund requires approval
		expiresAt := time.Now().Add(1 * time.Hour)

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
					"risk_level": t.assessRiskLevel(req.Amount),
					"amount":     req.Amount,
					"order_id":   req.OrderID,
				},
			},
		}, nil
	}

	// Low-value refund - process immediately
	result, err := t.processRefundInternal(req)
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

// assessRiskLevel returns a risk level for the refund
func (t *AsyncRefundTool) assessRiskLevel(amount float64) string {
	if amount > 500 {
		return "high"
	}
	if amount > 100 {
		return "medium"
	}
	return "low"
}

// processRefundInternal performs the actual refund processing
func (t *AsyncRefundTool) processRefundInternal(req RefundRequest) (json.RawMessage, error) {
	// Simulate processing delay
	time.Sleep(100 * time.Millisecond)

	// Return success result
	result := map[string]interface{}{
		"status":       "approved",
		"refund_id":    fmt.Sprintf("REF-%d", time.Now().Unix()),
		"processed_at": time.Now().Format(time.RFC3339),
		"amount":       req.Amount,
		"order_id":     req.OrderID,
	}

	return json.Marshal(result)
}

// --- Mock Approval Store ---

type mockApprovalStore struct {
	approvals map[string]*mockApproval
}

type mockApproval struct {
	result   json.RawMessage
	rejected bool
	reason   string
}

func newMockApprovalStore() *mockApprovalStore {
	return &mockApprovalStore{
		approvals: make(map[string]*mockApproval),
	}
}

func (s *mockApprovalStore) key(args json.RawMessage) string {
	return string(args)
}

func (s *mockApprovalStore) get(args json.RawMessage) *mockApproval {
	return s.approvals[s.key(args)]
}

func (s *mockApprovalStore) approve(toolName string, args json.RawMessage, result json.RawMessage) {
	s.approvals[s.key(args)] = &mockApproval{
		result:   result,
		rejected: false,
	}
}

func (s *mockApprovalStore) reject(toolName string, args json.RawMessage, reason string) {
	s.approvals[s.key(args)] = &mockApproval{
		rejected: true,
		reason:   reason,
	}
}
