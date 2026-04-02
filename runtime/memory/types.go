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

// Provenance indicates how a memory was created.
// Downstream systems (PII redaction, audit, retention) use this to apply
// context-appropriate policies. Stored in Memory.Metadata[MetaKeyProvenance].
type Provenance string

const (
	// MetaKeyProvenance is the well-known Metadata key for provenance.
	MetaKeyProvenance = "provenance"

	// ProvenanceUserRequested — user explicitly asked the agent to remember this.
	ProvenanceUserRequested Provenance = "user_requested"

	// ProvenanceAgentExtracted — the agent/pipeline extracted this from
	// conversation without an explicit user request (safe default).
	ProvenanceAgentExtracted Provenance = "agent_extracted"

	// ProvenanceSystemGenerated — created by system logic, not from user content.
	ProvenanceSystemGenerated Provenance = "system_generated"

	// ProvenanceOperatorCurated — manually created/edited by an operator.
	ProvenanceOperatorCurated Provenance = "operator_curated"
)

// SetProvenance sets the provenance metadata on a Memory, initializing
// the Metadata map if nil. This always overwrites any existing provenance
// value to prevent callers from spoofing provenance.
func (m *Memory) SetProvenance(p Provenance) {
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	m.Metadata[MetaKeyProvenance] = string(p)
}

// GetProvenance returns the provenance from metadata, or empty string if unset.
func (m *Memory) GetProvenance() Provenance {
	if m.Metadata == nil {
		return ""
	}
	if v, ok := m.Metadata[MetaKeyProvenance].(string); ok {
		return Provenance(v)
	}
	return ""
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
