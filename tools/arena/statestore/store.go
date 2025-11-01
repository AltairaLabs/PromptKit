package statestore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ErrNotFound is re-exported from runtime statestore
var ErrNotFound = runtimestore.ErrNotFound

// Feedback represents user feedback on conversation results
// This is a placeholder for future optimization features (v0.3.0)
type Feedback struct {
	ConversationID string                 `json:"conversation_id"`
	UserID         string                 `json:"user_id"`
	Rating         int                    `json:"rating"`
	Comments       string                 `json:"comments"`
	Categories     map[string]interface{} `json:"categories"`
	Timestamp      time.Time              `json:"timestamp"`
	Tags           []string               `json:"tags"`
}

// SelfPlayRoleInfo contains provider information for self-play roles
type SelfPlayRoleInfo struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Region   string `json:"region,omitempty"`
}

// RunMetadata contains Arena-specific execution metadata
type RunMetadata struct {
	RunID      string                 `json:"run_id"`
	PromptPack string                 `json:"prompt_pack,omitempty"`
	Region     string                 `json:"region"`
	ScenarioID string                 `json:"scenario_id"`
	ProviderID string                 `json:"provider_id"`
	Params     map[string]interface{} `json:"params,omitempty"`
	Commit     map[string]interface{} `json:"commit,omitempty"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	Duration   time.Duration          `json:"duration"`
	Error      string                 `json:"error,omitempty"`

	// Self-play metadata
	SelfPlay      bool              `json:"self_play,omitempty"`
	PersonaID     string            `json:"persona_id,omitempty"`
	AssistantRole *SelfPlayRoleInfo `json:"assistant_role,omitempty"`
	UserRole      *SelfPlayRoleInfo `json:"user_role,omitempty"`

	// Optimizer/feedback metadata
	UserFeedback *Feedback `json:"user_feedback,omitempty"`
	SessionTags  []string  `json:"session_tags,omitempty"`
}

// ValidationResult captures validation outcome for a turn
type ValidationResult struct {
	TurnIndex     int                    `json:"turn_index"`
	Timestamp     time.Time              `json:"timestamp"`
	ValidatorType string                 `json:"validator_type"`
	Passed        bool                   `json:"passed"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

// ArenaConversationState extends runtimestore.ConversationState with Arena-specific telemetry
// All telemetry is computed on-demand from the messages to avoid redundancy
type ArenaConversationState struct {
	runtimestore.ConversationState // Embed standard state

	// Arena-specific run metadata
	RunMetadata *RunMetadata `json:"run_metadata,omitempty"`
}

// ArenaStateStore is an in-memory state store with Arena-specific telemetry
type ArenaStateStore struct {
	conversations map[string]*ArenaConversationState
	mu            sync.RWMutex
}

// NewArenaStateStore creates a new Arena state store
func NewArenaStateStore() *ArenaStateStore {
	return &ArenaStateStore{
		conversations: make(map[string]*ArenaConversationState),
	}
}

// Save stores conversation state (implements Store interface)
// For Arena, this extracts trace data from metadata if present
func (s *ArenaStateStore) Save(ctx context.Context, state *runtimestore.ConversationState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	conversationID := state.ID

	// Deep clone the state to capture current state immutably
	clonedState := s.deepCloneConversationState(state)

	// Get or create arena state
	arenaState, exists := s.conversations[conversationID]
	if !exists {
		arenaState = &ArenaConversationState{
			ConversationState: *clonedState,
		}
		s.conversations[conversationID] = arenaState
	} else {
		// Update the embedded state with cloned version
		arenaState.ConversationState = *clonedState
	}

	return nil
}

// SaveWithTrace stores conversation state with execution trace (Arena-specific method)
// This is called by ArenaStateStoreSaveMiddleware to directly pass the trace
func (s *ArenaStateStore) SaveWithTrace(ctx context.Context, state *runtimestore.ConversationState, trace *pipeline.ExecutionTrace) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	conversationID := state.ID

	// Deep clone the state to capture current state immutably
	clonedState := s.deepCloneConversationState(state)

	// Attach trace to messages if provided
	if trace != nil && len(trace.LLMCalls) > 0 {
		s.attachTraceToMessages(clonedState, trace)
	}

	// Get or create arena state
	arenaState, exists := s.conversations[conversationID]
	if !exists {
		arenaState = &ArenaConversationState{
			ConversationState: *clonedState,
		}
		s.conversations[conversationID] = arenaState
	} else {
		// Update the embedded state with cloned version
		arenaState.ConversationState = *clonedState
	}

	return nil
}

// attachTraceToMessages attaches LLMCall data to message Meta fields using MessageIndex
func (s *ArenaStateStore) attachTraceToMessages(state *runtimestore.ConversationState, trace *pipeline.ExecutionTrace) {
	for _, llmCall := range trace.LLMCalls {
		// Use MessageIndex to find the corresponding message
		if llmCall.MessageIndex >= 0 && llmCall.MessageIndex < len(state.Messages) {
			msg := &state.Messages[llmCall.MessageIndex]

			// Initialize Meta if needed
			if msg.Meta == nil {
				msg.Meta = make(map[string]interface{})
			}

			// Attach LLMCall data to message metadata
			msg.Meta["_llm_trace"] = map[string]interface{}{
				"sequence":      llmCall.Sequence,
				"message_index": llmCall.MessageIndex,
				"started_at":    llmCall.StartedAt,
				"duration_ms":   llmCall.Duration.Milliseconds(),
				"cost":          llmCall.Cost,
				"tool_calls":    llmCall.ToolCalls,
			}

			// Optionally include raw request/response if they're present (for debugging)
			if llmCall.Request != nil {
				msg.Meta["_llm_raw_request"] = llmCall.Request
			}
			if llmCall.RawResponse != nil {
				msg.Meta["_llm_raw_response"] = llmCall.RawResponse
			}
		}
	}
}

// deepCloneConversationState creates a deep copy of ConversationState including all messages
func (s *ArenaStateStore) deepCloneConversationState(state *runtimestore.ConversationState) *runtimestore.ConversationState {
	if state == nil {
		return nil
	}

	cloned := &runtimestore.ConversationState{
		ID:             state.ID,
		UserID:         state.UserID,
		SystemPrompt:   state.SystemPrompt,
		TokenCount:     state.TokenCount,
		LastAccessedAt: state.LastAccessedAt,
	}

	// Deep clone Messages slice
	if len(state.Messages) > 0 {
		cloned.Messages = make([]types.Message, len(state.Messages))
		for i, msg := range state.Messages {
			cloned.Messages[i] = s.deepCloneMessage(&msg)
		}
	}

	// Deep clone Summaries slice
	if len(state.Summaries) > 0 {
		cloned.Summaries = make([]runtimestore.Summary, len(state.Summaries))
		copy(cloned.Summaries, state.Summaries)
	}

	// Deep clone Metadata map
	if len(state.Metadata) > 0 {
		cloned.Metadata = make(map[string]interface{}, len(state.Metadata))
		for k, v := range state.Metadata {
			cloned.Metadata[k] = s.deepCloneValue(v)
		}
	}

	return cloned
}

// deepCloneMessage creates a deep copy of a Message including all nested data
func (s *ArenaStateStore) deepCloneMessage(msg *types.Message) types.Message {
	if msg == nil {
		return types.Message{}
	}

	cloned := types.Message{
		Role:      msg.Role,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
		LatencyMs: msg.LatencyMs,
		Source:    msg.Source, // Preserve Source field
	}

	// Deep clone ToolCalls slice
	if len(msg.ToolCalls) > 0 {
		cloned.ToolCalls = make([]types.MessageToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			cloned.ToolCalls[i] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: append(json.RawMessage(nil), tc.Args...), // Clone the raw JSON bytes
			}
		}
	}

	// Deep clone ToolResult
	if msg.ToolResult != nil {
		cloned.ToolResult = &types.MessageToolResult{
			ID:        msg.ToolResult.ID,
			Name:      msg.ToolResult.Name,
			Content:   msg.ToolResult.Content,
			Error:     msg.ToolResult.Error,
			LatencyMs: msg.ToolResult.LatencyMs,
		}
	}

	// Deep clone CostInfo
	if msg.CostInfo != nil {
		cloned.CostInfo = &types.CostInfo{
			InputTokens:   msg.CostInfo.InputTokens,
			OutputTokens:  msg.CostInfo.OutputTokens,
			CachedTokens:  msg.CostInfo.CachedTokens,
			InputCostUSD:  msg.CostInfo.InputCostUSD,
			OutputCostUSD: msg.CostInfo.OutputCostUSD,
			CachedCostUSD: msg.CostInfo.CachedCostUSD,
			TotalCost:     msg.CostInfo.TotalCost,
		}
	}

	// Deep clone Meta map (this is critical for assertions!)
	if len(msg.Meta) > 0 {
		cloned.Meta = make(map[string]interface{}, len(msg.Meta))
		for k, v := range msg.Meta {
			cloned.Meta[k] = s.deepCloneValue(v)
		}
	}

	// Deep clone Validations slice
	if len(msg.Validations) > 0 {
		cloned.Validations = make([]types.ValidationResult, len(msg.Validations))
		for i, vr := range msg.Validations {
			cloned.Validations[i] = types.ValidationResult{
				ValidatorType: vr.ValidatorType,
				Passed:        vr.Passed,
				Timestamp:     vr.Timestamp,
			}
			// Deep clone Details map
			if len(vr.Details) > 0 {
				cloned.Validations[i].Details = make(map[string]interface{}, len(vr.Details))
				for k, v := range vr.Details {
					cloned.Validations[i].Details[k] = s.deepCloneValue(v)
				}
			}
		}
	}

	return cloned
}

// deepCloneValue attempts to deep clone various types commonly found in metadata/meta maps
func (s *ArenaStateStore) deepCloneValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		cloned := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			cloned[k] = s.deepCloneValue(v2)
		}
		return cloned
	case []interface{}:
		cloned := make([]interface{}, len(val))
		for i, v2 := range val {
			cloned[i] = s.deepCloneValue(v2)
		}
		return cloned
	case []string:
		cloned := make([]string, len(val))
		copy(cloned, val)
		return cloned
	case json.RawMessage:
		return append(json.RawMessage(nil), val...)
	default:
		// For primitive types (string, int, float, bool, etc.), direct assignment is fine
		// since they're copied by value
		return v
	}
}

// Load retrieves conversation state (implements Store interface)
func (s *ArenaStateStore) Load(ctx context.Context, conversationID string) (*runtimestore.ConversationState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	arenaState, exists := s.conversations[conversationID]
	if !exists {
		return nil, ErrNotFound
	}

	// Return a copy of the embedded standard state
	stateCopy := arenaState.ConversationState

	// Ensure Metadata is never nil to prevent panics
	if stateCopy.Metadata == nil {
		stateCopy.Metadata = make(map[string]interface{})
	}

	return &stateCopy, nil
}

// Delete removes conversation state
func (s *ArenaStateStore) Delete(ctx context.Context, conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conversations, conversationID)
	return nil
}

// GetArenaState retrieves the full Arena state including telemetry
func (s *ArenaStateStore) GetArenaState(ctx context.Context, conversationID string) (*ArenaConversationState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	arenaState, exists := s.conversations[conversationID]
	if !exists {
		return nil, fmt.Errorf("conversation %s not found", conversationID)
	}

	return arenaState, nil
}

// RunResult represents the full result structure for JSON compatibility
// This type mirrors engine.RunResult to avoid circular dependencies
type RunResult struct {
	RunID      string                  `json:"RunID"`
	PromptPack string                  `json:"PromptPack"`
	Region     string                  `json:"Region"`
	ScenarioID string                  `json:"ScenarioID"`
	ProviderID string                  `json:"ProviderID"`
	Params     map[string]interface{}  `json:"Params"`
	Messages   []types.Message         `json:"Messages"`
	Commit     map[string]interface{}  `json:"Commit"`
	Cost       types.CostInfo          `json:"Cost"`
	ToolStats  *types.ToolStats        `json:"ToolStats"`
	Violations []types.ValidationError `json:"Violations"`
	StartTime  time.Time               `json:"StartTime"`
	EndTime    time.Time               `json:"EndTime"`
	Duration   time.Duration           `json:"Duration"`
	Error      string                  `json:"Error"`
	SelfPlay   bool                    `json:"SelfPlay"`
	PersonaID  string                  `json:"PersonaID"`

	UserFeedback  *Feedback   `json:"UserFeedback"`
	SessionTags   []string    `json:"SessionTags"`
	AssistantRole interface{} `json:"AssistantRole"` // Using interface{} to avoid circular import
	UserRole      interface{} `json:"UserRole"`
}
