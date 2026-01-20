// Package tools provides HITL (Human-in-the-Loop) tool support.
package tools

import (
	"encoding/json"
	"fmt"
	"sync"
)

// PendingResult is returned by async tool handlers to indicate
// that the tool execution requires external approval.
//
// Example:
//
//	conv.OnToolAsync("process_refund", func(args map[string]any) PendingResult {
//	    amount := args["amount"].(float64)
//	    if amount > 1000 {
//	        return PendingResult{
//	            Reason:  "high_value_refund",
//	            Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
//	        }
//	    }
//	    // Return empty to proceed immediately
//	    return PendingResult{}
//	})
type PendingResult struct {
	// Reason is a machine-readable code for why approval is needed.
	// Examples: "high_value", "sensitive_action", "rate_limited"
	Reason string

	// Message is a human-readable explanation for the approval requirement.
	// This should be suitable for display to an approver.
	Message string
}

// IsPending returns true if this result requires approval.
func (p PendingResult) IsPending() bool {
	return p.Reason != "" || p.Message != ""
}

// AsyncToolHandler is a function that may require approval before execution.
// Return a non-empty PendingResult to indicate approval is needed.
// Return an empty PendingResult{} to proceed immediately.
type AsyncToolHandler func(args map[string]any) PendingResult

// PendingToolCall represents a tool call awaiting approval.
type PendingToolCall struct {
	// Unique identifier for this pending call
	ID string `json:"id"`

	// Tool name
	Name string `json:"name"`

	// Arguments passed to the tool
	Arguments map[string]any `json:"arguments"`

	// Reason the tool requires approval (from PendingResult)
	Reason string `json:"reason"`

	// Human-readable message (from PendingResult)
	Message string `json:"message"`

	// The underlying handler to execute if approved
	handler func(args map[string]any) (any, error)
}

// PendingStore manages pending tool calls for a conversation.
type PendingStore struct {
	pending map[string]*PendingToolCall
	mu      sync.RWMutex
}

// NewPendingStore creates a new pending tool store.
func NewPendingStore() *PendingStore {
	return &PendingStore{
		pending: make(map[string]*PendingToolCall),
	}
}

// Add stores a pending tool call.
func (s *PendingStore) Add(call *PendingToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[call.ID] = call
}

// Get retrieves a pending tool call by ID.
func (s *PendingStore) Get(id string) (*PendingToolCall, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	call, ok := s.pending[id]
	return call, ok
}

// Remove deletes a pending tool call.
func (s *PendingStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, id)
}

// List returns all pending tool calls.
func (s *PendingStore) List() []*PendingToolCall {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*PendingToolCall, 0, len(s.pending))
	for _, call := range s.pending {
		result = append(result, call)
	}
	return result
}

// Clear removes all pending tool calls.
func (s *PendingStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = make(map[string]*PendingToolCall)
}

// Len returns the number of pending calls.
func (s *PendingStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pending)
}

// ToolResolution represents the result of resolving a pending tool.
type ToolResolution struct {
	// The resolved tool call ID
	ID string

	// Result if approved and executed
	Result any

	// ResultJSON is the JSON-encoded result
	ResultJSON json.RawMessage

	// Error if execution failed
	Error error

	// Rejected is true if the tool was rejected
	Rejected bool

	// RejectionReason explains why the tool was rejected
	RejectionReason string
}

// Resolve executes an approved pending tool call.
func (s *PendingStore) Resolve(id string) (*ToolResolution, error) {
	s.mu.Lock()
	call, ok := s.pending[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("no pending tool call with ID %q", id)
	}
	delete(s.pending, id)
	s.mu.Unlock()

	// Execute the handler
	result, err := call.handler(call.Arguments)
	if err != nil {
		return &ToolResolution{
			ID:    id,
			Error: err,
		}, nil
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return &ToolResolution{
			ID:    id,
			Error: fmt.Errorf("failed to serialize result: %w", err),
		}, nil
	}

	return &ToolResolution{
		ID:         id,
		Result:     result,
		ResultJSON: resultJSON,
	}, nil
}

// Reject marks a pending tool call as rejected.
func (s *PendingStore) Reject(id, reason string) (*ToolResolution, error) {
	s.mu.Lock()
	_, ok := s.pending[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("no pending tool call with ID %q", id)
	}
	delete(s.pending, id)
	s.mu.Unlock()

	return &ToolResolution{
		ID:              id,
		Rejected:        true,
		RejectionReason: reason,
	}, nil
}

// ResolvedStore tracks tool call resolutions that haven't been processed by Continue().
// This allows the Continue() method to send proper tool result messages to the LLM.
type ResolvedStore struct {
	resolutions []*ToolResolution
	mu          sync.Mutex
}

// NewResolvedStore creates a new resolved tool store.
func NewResolvedStore() *ResolvedStore {
	return &ResolvedStore{
		resolutions: make([]*ToolResolution, 0),
	}
}

// Add stores a resolved tool call.
func (s *ResolvedStore) Add(resolution *ToolResolution) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolutions = append(s.resolutions, resolution)
}

// PopAll returns all resolutions and clears the store.
// Used by Continue() to get all pending tool results.
func (s *ResolvedStore) PopAll() []*ToolResolution {
	s.mu.Lock()
	defer s.mu.Unlock()
	resolutions := s.resolutions
	s.resolutions = make([]*ToolResolution, 0)
	return resolutions
}

// Len returns the number of stored resolutions.
func (s *ResolvedStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.resolutions)
}
