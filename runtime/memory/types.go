// Package memory defines interfaces and types for agentic memory —
// cross-session knowledge that persists beyond a single conversation.
//
// PromptKit defines interfaces and an in-memory test store. Production
// implementations (vector search, graph retrieval, compliance) are
// provided by platform layers like Omnia.
package memory

import "time"

// Memory represents a single memory unit.
// Deliberately thin — domain-specific concerns (purpose, trust model,
// sensitivity) belong in the store implementation, not the type.
type Memory struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`               // Free-form: "preference", "episodic", "code_symbol", etc.
	Content    string            `json:"content"`            // Natural language summary
	Metadata   map[string]any    `json:"metadata,omitempty"` // Structured data, store-specific extensions
	Confidence float64           `json:"confidence"`         // 0.0-1.0
	Scope      map[string]string `json:"scope"`              // Scoping keys: {"user_id": "x", "workspace_id": "y"}
	SessionID  string            `json:"session_id,omitempty"`
	TurnRange  [2]int            `json:"turn_range,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	AccessedAt time.Time         `json:"accessed_at"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
}

// RetrieveOptions configures a memory retrieval query.
type RetrieveOptions struct {
	Types         []string // Filter by memory type (empty = all)
	Limit         int      // Max results (0 = store default)
	MinConfidence float64  // Minimum confidence threshold (0 = no filter)
}

// ListOptions configures a memory list query.
type ListOptions struct {
	Types  []string
	Limit  int
	Offset int
}
