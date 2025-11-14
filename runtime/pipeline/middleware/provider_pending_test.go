package middleware

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestProviderMiddleware_StopsOnPendingTools tests that the provider middleware
// stops execution when a tool returns pending status (doesn't make another LLM call)
func TestProviderMiddleware_StopsOnPendingTools(t *testing.T) {
	// Create a mock async tool that returns pending
	approvalStore := &mockApprovalStore{
		approvals: make(map[string]*mockApproval),
	}

	registry := tools.NewRegistry()
	asyncTool := &mockAsyncRefundTool{
		approvalStore: approvalStore,
	}

	// Register the tool
	registry.RegisterExecutor(asyncTool)
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "process_refund",
		Description: "Process a refund",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"amount": {"type": "number"}}}`),
	})
	if err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	// Create a mock provider that will be called
	callCount := 0
	mockProvider := &mockProviderForPendingTest{
		onCall: func() (providers.PredictionResponse, error) {
			callCount++
			if callCount == 1 {
				// First call: LLM wants to call the tool
				return providers.PredictionResponse{
					Content: "I'll process that refund.",
					ToolCalls: []types.MessageToolCall{
						{
							ID:   "call_1",
							Name: "process_refund",
							Args: json.RawMessage(`{"amount": 500}`),
						},
					},
				}, nil
			}
			// Second call should NOT happen when tool is pending
			t.Error("Provider was called a second time after tool returned pending - should have stopped!")
			return providers.PredictionResponse{
				Content: "This should not be generated",
			}, nil
		},
	}

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: pipeline.ExecutionTrace{
			LLMCalls: []pipeline.LLMCall{},
			Events:   []pipeline.TraceEvent{},
		},
	}

	// Add user message
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "user",
		Content: "I need a refund of $500",
	})

	// Execute the middleware
	err = executeNonStreaming(execCtx, mockProvider, registry, nil, nil)

	// Verify: Should have exactly 1 LLM call (not 2)
	if callCount != 1 {
		t.Errorf("Expected 1 LLM call, got %d", callCount)
	}

	// Verify: Should have pending tools in context
	if !execCtx.HasPendingToolCalls() {
		t.Error("Expected HasPendingToolCalls() to be true")
	}

	// Verify: Should have pending metadata
	if _, hasPendingMeta := execCtx.Metadata["pending_tools"]; !hasPendingMeta {
		t.Error("Expected pending_tools metadata to be set")
	}

	// Verify: Error should indicate pending tools
	if err == nil {
		t.Error("Expected error indicating pending tools, got nil")
	} else if err.Error() != "execution paused: pending tool calls require approval" {
		t.Errorf("Expected specific pending error, got: %v", err)
	}
}

// TestProviderMiddleware_ContinuesAfterApproval tests that execution continues
// normally after a pending tool is approved
func TestProviderMiddleware_ContinuesAfterApproval(t *testing.T) {
	// Create approval store with pre-approved result
	approvalStore := &mockApprovalStore{
		approvals: make(map[string]*mockApproval),
	}
	// Note: JSON formatting must match exactly - no spaces
	approvalStore.approve(
		"process_refund",
		json.RawMessage(`{"amount":500}`), // No spaces in JSON
		json.RawMessage(`{"status":"approved","refund_id":"REF-123"}`),
	)

	registry := tools.NewRegistry()
	asyncTool := &mockAsyncRefundTool{
		approvalStore: approvalStore,
	}

	registry.RegisterExecutor(asyncTool)
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "process_refund",
		Description: "Process a refund",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"amount": {"type": "number"}}}`),
	})
	if err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	// Mock provider with 2 expected calls
	callCount := 0
	mockProvider := &mockProviderForPendingTest{
		onCall: func() (providers.PredictionResponse, error) {
			callCount++
			if callCount == 1 {
				return providers.PredictionResponse{
					Content: "I'll process that refund.",
					ToolCalls: []types.MessageToolCall{
						{
							ID:   "call_1",
							Name: "process_refund",
							Args: json.RawMessage(`{"amount":500}`), // Must match approval store key - no spaces
						},
					},
				}, nil
			}
			// Second call: LLM responds after tool completes
			return providers.PredictionResponse{
				Content: "Your refund has been processed successfully.",
			}, nil
		},
	}

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: pipeline.ExecutionTrace{
			LLMCalls: []pipeline.LLMCall{},
			Events:   []pipeline.TraceEvent{},
		},
	}

	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "user",
		Content: "I need a refund of $500",
	})

	// Execute
	err = executeNonStreaming(execCtx, mockProvider, registry, nil, nil)

	// Should succeed (no pending tools since approval exists)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should have made 2 LLM calls
	if callCount != 2 {
		t.Errorf("Expected 2 LLM calls, got %d", callCount)
	}

	// Should NOT have pending tools
	if execCtx.HasPendingToolCalls() {
		t.Error("Expected HasPendingToolCalls() to be false after approval")
	}
}

// Mock types for testing

type mockProviderForPendingTest struct {
	onCall func() (providers.PredictionResponse, error)
}

func (m *mockProviderForPendingTest) ID() string {
	return "mock-pending-test"
}

func (m *mockProviderForPendingTest) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return m.onCall()
}

func (m *mockProviderForPendingTest) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *mockProviderForPendingTest) SupportsStreaming() bool {
	return false
}

func (m *mockProviderForPendingTest) ShouldIncludeRawOutput() bool {
	return false
}

func (m *mockProviderForPendingTest) Close() error {
	return nil
}

func (m *mockProviderForPendingTest) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

func (m *mockProviderForPendingTest) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
	return nil, nil
}

func (m *mockProviderForPendingTest) PredictWithTools(ctx context.Context, req providers.PredictionRequest, tooling interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
	resp, err := m.onCall()
	return resp, resp.ToolCalls, err
}

type mockAsyncRefundTool struct {
	approvalStore *mockApprovalStore
}

func (t *mockAsyncRefundTool) Name() string {
	return "mock-static"
}

func (t *mockAsyncRefundTool) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

func (t *mockAsyncRefundTool) ExecuteAsync(descriptor *tools.ToolDescriptor, args json.RawMessage) (*tools.ToolExecutionResult, error) {
	// Check if approval exists
	if approval := t.approvalStore.get(args); approval != nil {
		if approval.rejected {
			return &tools.ToolExecutionResult{
				Status: tools.ToolStatusFailed,
				Error:  approval.reason,
			}, nil
		}
		// Approved - return complete
		return &tools.ToolExecutionResult{
			Status:  tools.ToolStatusComplete,
			Content: approval.result,
		}, nil
	}

	// No approval - return pending
	return &tools.ToolExecutionResult{
		Status: tools.ToolStatusPending,
		PendingInfo: &tools.PendingToolInfo{
			Reason:   "requires_approval",
			Message:  "Refund requires supervisor approval",
			ToolName: descriptor.Name,
			Args:     args,
		},
	}, nil
}

type mockApprovalStore struct {
	approvals map[string]*mockApproval
}

type mockApproval struct {
	result   json.RawMessage
	rejected bool
	reason   string
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
