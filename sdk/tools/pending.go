// Package tools provides HITL (Human-in-the-Loop) tool support.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// DefaultPendingTTL is the default time-to-live for pending tool calls.
	// Entries older than this are automatically removed (by the in-memory
	// cleanup goroutine, or by the backend's native expiry for durable stores).
	DefaultPendingTTL = 5 * time.Minute

	// DefaultMaxPending is the default maximum number of pending tool calls a
	// MemoryPendingStore holds simultaneously. New adds are rejected when full.
	DefaultMaxPending = 1000
)

// ErrPendingStoreFull is returned when the store has reached its maximum capacity.
var ErrPendingStoreFull = errors.New("pending store is full")

// ErrPendingAlreadyResolved is returned by a resolve/reject when the pending
// call is no longer present — it was already claimed by another caller (e.g. a
// second instance of the same agent) or expired. It is the signal that this
// caller lost the single-winner race and must not act on the call.
var ErrPendingAlreadyResolved = errors.New("pending tool call already resolved or expired")

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

// ExecFunc is a registered tool execution handler. On resolve, the Conversation
// recovers the ExecFunc for a pending call by tool name (a persisted call cannot
// carry the closure) and runs it with the approved arguments.
type ExecFunc func(args map[string]any) (any, error)

// PendingToolCall represents a tool call awaiting approval. It is pure data: it
// carries no execution closure, so it can be serialized into a durable store and
// resolved by a different process. The execution handler is recovered by Name at
// resolve time (see Conversation.ResolveTool).
type PendingToolCall struct {
	// Unique identifier for this pending call (the provider tool call ID).
	ID string `json:"id"`

	// ConversationID scopes the call so a store shared across conversations can
	// isolate queues and list one conversation's pending calls.
	ConversationID string `json:"conversation_id"`

	// Tool name — also the key used to recover the execution handler on resolve.
	Name string `json:"name"`

	// Arguments the model proposed for the call.
	Arguments map[string]any `json:"arguments"`

	// Reason the tool requires approval (from PendingResult).
	Reason string `json:"reason"`

	// Human-readable message (from PendingResult).
	Message string `json:"message"`

	// CreatedAt tracks when this entry was added, for TTL expiration.
	CreatedAt time.Time `json:"created_at"`
}

// PendingStore persists tool calls awaiting human approval.
//
// It mirrors statestore.Store: read-shaped for inspection, with a single
// atomic take primitive for resolution. Implementations MUST make Claim atomic
// — at most one caller may successfully claim a given id — so that concurrent
// instances of the same agent cannot double-resolve a call. Get and List are
// read-only and carry no such guarantee.
//
// The store never executes tool handlers; resolution (running the recovered
// handler) happens in the Conversation. This keeps memory and durable backends
// on one identical resolution path.
type PendingStore interface {
	// Add persists a pending call. Returns ErrPendingStoreFull if a bounded
	// store is at capacity.
	Add(ctx context.Context, call *PendingToolCall) error

	// Get returns a read-only view of a pending call, or ok=false if absent.
	Get(ctx context.Context, convID, id string) (*PendingToolCall, bool, error)

	// List returns all pending calls for a conversation.
	List(ctx context.Context, convID string) ([]*PendingToolCall, error)

	// Claim atomically removes and returns a pending call. ok=false means the
	// call was already claimed by another caller or never existed. This is the
	// single-winner resolution gate for both approve and reject.
	Claim(ctx context.Context, convID, id string) (*PendingToolCall, bool, error)
}

// Closer is an optional PendingStore capability. In-memory stores expose it to
// stop their cleanup goroutine; the SDK type-asserts for it on Close. Durable
// stores that hold no local resources need not implement it.
type Closer interface {
	Close() error
}

// ToolResolution represents the result of resolving a pending tool.
type ToolResolution struct {
	// The resolved tool call ID
	ID string

	// Result if approved and executed
	Result any

	// ResultJSON is the JSON-encoded result
	ResultJSON json.RawMessage

	// Parts contains multimodal content parts for the tool result.
	// When set, these are used directly as MessageToolResult.Parts,
	// taking precedence over ResultJSON.
	Parts []types.ContentPart

	// Error if execution failed
	Error error

	// Rejected is true if the tool was rejected
	Rejected bool

	// RejectionReason explains why the tool was rejected
	RejectionReason string

	// Arguments are the effective arguments the handler executed with. When the
	// call was approved with edits, this is the original arguments with the
	// reviewer's overrides merged in.
	Arguments map[string]any

	// Edited is true when the call was approved with reviewer-supplied argument
	// overrides (see ResolveApproved). Provides an audit trail distinguishing an
	// as-proposed approval from an edited one.
	Edited bool
}

// ResolveApproved runs handler with the call's arguments (shallow-merged with
// overrides) and returns the ToolResolution. It is pure — it touches no store —
// so the Conversation calls it after atomically claiming the call and recovering
// the handler by name.
//
// Overrides are shallow-merged over a copy of the original arguments: keys in
// overrides replace the originals, absent keys are preserved. A nil or empty
// overrides map executes the call exactly as proposed. The original call is
// never mutated.
func ResolveApproved(call *PendingToolCall, handler ExecFunc, overrides map[string]any) *ToolResolution {
	args := make(map[string]any, len(call.Arguments)+len(overrides))
	maps.Copy(args, call.Arguments)
	maps.Copy(args, overrides)
	edited := len(overrides) > 0

	result, err := handler(args)
	if err != nil {
		return &ToolResolution{ID: call.ID, Arguments: args, Edited: edited, Error: err}
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return &ToolResolution{
			ID:        call.ID,
			Arguments: args,
			Edited:    edited,
			Error:     fmt.Errorf("failed to serialize result: %w", err),
		}
	}

	return &ToolResolution{
		ID:         call.ID,
		Result:     result,
		ResultJSON: resultJSON,
		Arguments:  args,
		Edited:     edited,
	}
}

// ResolveRejected builds a rejected ToolResolution for a claimed call.
func ResolveRejected(id, reason string) *ToolResolution {
	return &ToolResolution{ID: id, Rejected: true, RejectionReason: reason}
}

// ResolvedStore tracks tool call resolutions that haven't been processed by Continue().
// This allows the Continue() method to send proper tool result messages to the LLM.
// It is transient and per-conversation — resolutions are consumed immediately by
// Continue/ContinueDuplex and never persisted.
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
