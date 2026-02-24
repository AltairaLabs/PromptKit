package hooks

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Decision is the result of a hook evaluation.
type Decision struct {
	Allow    bool
	Reason   string
	Metadata map[string]any
}

// Allow is the zero-cost approval decision.
var Allow = Decision{Allow: true} //nolint:gochecknoglobals // convenience sentinel

// Deny creates a denial decision with a reason.
func Deny(reason string) Decision {
	return Decision{Allow: false, Reason: reason}
}

// DenyWithMetadata creates a denial decision with a reason and metadata.
func DenyWithMetadata(reason string, metadata map[string]any) Decision {
	return Decision{Allow: false, Reason: reason, Metadata: metadata}
}

// ProviderRequest describes an LLM call about to be made.
type ProviderRequest struct {
	ProviderID   string
	Model        string
	Messages     []types.Message
	SystemPrompt string
	Round        int
	Metadata     map[string]any
}

// ProviderResponse describes a completed LLM call.
type ProviderResponse struct {
	ProviderID string
	Model      string
	Message    types.Message
	Round      int
	LatencyMs  int64
}

// ToolRequest describes a tool call about to be executed.
type ToolRequest struct {
	Name   string
	Args   json.RawMessage
	CallID string
}

// ToolResponse describes a completed tool execution.
type ToolResponse struct {
	Name      string
	CallID    string
	Content   string
	Error     string
	LatencyMs int64
}

// SessionEvent carries context for session lifecycle hooks.
type SessionEvent struct {
	SessionID      string
	ConversationID string
	Messages       []types.Message
	TurnIndex      int
	Metadata       map[string]any
}
