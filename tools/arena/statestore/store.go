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
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// ErrNotFound is re-exported from runtime statestore
var ErrNotFound = runtimestore.ErrNotFound

// roleAssistant is the role constant for assistant messages.
const roleAssistant = "assistant"

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

// A2AAgentInfo contains metadata about an A2A agent for report rendering.
type A2AAgentInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Skills      []A2ASkillInfo `json:"skills,omitempty"`
}

// A2ASkillInfo contains metadata about a single A2A agent skill.
type A2ASkillInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
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

	// Session recording path (if recording is enabled)
	RecordingPath string `json:"recording_path,omitempty"`

	// Conversation-level assertions (evaluated after conversation completes)
	ConversationAssertionResults []ConversationValidationResult `json:"conv_assertions_results,omitempty"`

	// A2A agent metadata (populated from config for report rendering)
	A2AAgents []A2AAgentInfo `json:"a2a_agents,omitempty"`

	// TrialResults holds aggregated statistics when a scenario is run with Trials > 1.
	TrialResults interface{} `json:"trial_results,omitempty"`
}

// ValidationResult captures validation outcome for a turn
type ValidationResult struct {
	TurnIndex     int                    `json:"turn_index"`
	Timestamp     time.Time              `json:"timestamp"`
	ValidatorType string                 `json:"validator_type"`
	Passed        bool                   `json:"passed"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

// ConversationValidationResult is an alias for assertions.ConversationValidationResult
// to avoid import cycles and maintain clean separation between statestore and assertions.
type ConversationValidationResult = assertions.ConversationValidationResult

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

// Save stores conversation state (implements Store interface).
// Uses incremental cloning: only new messages (appended since the last Save)
// are deep-cloned. Previously stored messages are already immutable snapshots
// and are reused, reducing per-Save cost from O(N) to O(delta).
func (s *ArenaStateStore) Save(ctx context.Context, state *runtimestore.ConversationState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.saveStateLocked(state, nil)
	return nil
}

// SaveWithTrace stores conversation state with execution trace (Arena-specific method).
// This is called by ArenaStateStoreSaveMiddleware to directly pass the trace.
// LLM call traces (_llm_trace) are always attached to messages when trace data is available.
func (s *ArenaStateStore) SaveWithTrace(
	ctx context.Context,
	state *runtimestore.ConversationState,
	trace *pipeline.ExecutionTrace,
) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.saveStateLocked(state, trace)
	return nil
}

// saveStateLocked is the shared implementation for Save and SaveWithTrace.
// It performs incremental message cloning: messages already stored are reused,
// and only new messages (from storedCount onward) are deep-cloned.
func (s *ArenaStateStore) saveStateLocked(
	state *runtimestore.ConversationState,
	trace *pipeline.ExecutionTrace,
) {
	conversationID := state.ID

	arenaState, exists := s.conversations[conversationID]
	if !exists {
		// First save — full clone required.
		clonedState := s.deepCloneConversationState(state)
		if trace != nil && len(trace.LLMCalls) > 0 {
			s.attachTraceToMessages(clonedState, trace)
		}
		s.conversations[conversationID] = &ArenaConversationState{
			ConversationState: *clonedState,
		}
		return
	}

	// Incremental save: reuse stored messages, clone only new/mutated ones.
	arenaState.Messages = s.incrementalCloneMessages(arenaState.Messages, state.Messages)
	s.updateNonMessageFields(arenaState, state)

	if trace != nil && len(trace.LLMCalls) > 0 {
		s.attachTraceToMessages(&arenaState.ConversationState, trace)
	}
}

// incrementalCloneMessages builds a message slice reusing unchanged stored messages
// and deep-cloning only new or mutated ones. This reduces per-Save cost from O(N)
// to O(delta) for the common append-only case.
func (s *ArenaStateStore) incrementalCloneMessages(stored, incoming []types.Message) []types.Message {
	if len(incoming) == 0 {
		return nil
	}
	messages := make([]types.Message, len(incoming))
	reuseLimit := min(len(stored), len(incoming))
	for i := 0; i < reuseLimit; i++ {
		if stored[i].Content == incoming[i].Content &&
			len(stored[i].Meta) == len(incoming[i].Meta) {
			messages[i] = stored[i]
		} else {
			messages[i] = s.deepCloneMessage(&incoming[i])
		}
	}
	for i := reuseLimit; i < len(incoming); i++ {
		messages[i] = s.deepCloneMessage(&incoming[i])
	}
	return messages
}

// updateNonMessageFields copies non-message scalar and small collection fields
// from the incoming state to the stored arena state.
func (s *ArenaStateStore) updateNonMessageFields(
	arenaState *ArenaConversationState,
	state *runtimestore.ConversationState,
) {
	arenaState.ID = state.ID
	arenaState.UserID = state.UserID
	arenaState.SystemPrompt = state.SystemPrompt
	arenaState.TokenCount = state.TokenCount
	arenaState.LastAccessedAt = state.LastAccessedAt

	if len(state.Summaries) > 0 {
		arenaState.Summaries = make([]runtimestore.Summary, len(state.Summaries))
		copy(arenaState.Summaries, state.Summaries)
	} else {
		arenaState.Summaries = nil
	}

	if len(state.Metadata) > 0 {
		arenaState.Metadata = make(map[string]interface{}, len(state.Metadata))
		for k, v := range state.Metadata {
			arenaState.Metadata[k] = s.deepCloneValue(v)
		}
	} else {
		arenaState.Metadata = nil
	}
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

// Fork creates a copy of the state with a new session ID
// This is a no-op for the arena state store as forking is handled at the pipeline level
// NOSONAR: Intentionally empty - forking is handled at the pipeline level
func (s *ArenaStateStore) Fork(ctx context.Context, sourceID, newID string) error {
	return nil
}

// UpdateLastAssistantMessage updates the metadata of the last assistant message.
// This is used by duplex mode to attach assertion results to messages after evaluation.
//
// TODO(perf): Content-based message matching is O(N) per conversation. Consider passing
// conversation ID for direct lookup instead of scanning all conversations.
func (s *ArenaStateStore) UpdateLastAssistantMessage(msg *types.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the conversation that contains this message (by matching content)
	for _, arenaState := range s.conversations {
		if s.updateMessageInConversation(arenaState.Messages, msg) {
			return
		}
	}
}

// updateMessageInConversation finds and updates a matching assistant message.
// Returns true if a message was updated.
func (s *ArenaStateStore) updateMessageInConversation(msgs []types.Message, msg *types.Message) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if s.isMatchingAssistantMessage(&msgs[i], msg) {
			s.mergeMessageMeta(&msgs[i], msg)
			return true
		}
	}
	return false
}

// isMatchingAssistantMessage checks if two messages match by role and content.
func (s *ArenaStateStore) isMatchingAssistantMessage(existing, updated *types.Message) bool {
	return existing.Role == roleAssistant && existing.Content == updated.Content
}

// mergeMessageMeta copies metadata from source to target message.
func (s *ArenaStateStore) mergeMessageMeta(target, source *types.Message) {
	if source.Meta == nil {
		return
	}

	if target.Meta == nil {
		target.Meta = make(map[string]interface{})
	}

	for k, v := range source.Meta {
		target.Meta[k] = v
	}
}

// deepCloneConversationState creates a deep copy of ConversationState including all messages
func (s *ArenaStateStore) deepCloneConversationState(
	state *runtimestore.ConversationState,
) *runtimestore.ConversationState {
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
		Source:    msg.Source,
	}

	s.cloneMessageParts(&cloned, msg)
	s.cloneMessageToolCalls(&cloned, msg)
	s.cloneMessageToolResult(&cloned, msg)
	s.cloneMessageCostInfo(&cloned, msg)
	s.cloneMessageMeta(&cloned, msg)
	s.cloneMessageValidations(&cloned, msg)

	return cloned
}

// cloneMessageParts clones the Parts slice (multimodal content) with deep copy of pointer fields
func (s *ArenaStateStore) cloneMessageParts(cloned *types.Message, msg *types.Message) {
	if len(msg.Parts) > 0 {
		cloned.Parts = make([]types.ContentPart, len(msg.Parts))
		for i, part := range msg.Parts {
			cloned.Parts[i] = s.deepCloneContentPart(part)
		}
	}
}

// deepCloneContentPart creates a deep copy of a ContentPart including all pointer fields
func (s *ArenaStateStore) deepCloneContentPart(part types.ContentPart) types.ContentPart {
	cp := types.ContentPart{
		Type: part.Type,
	}
	if part.Text != nil {
		txt := *part.Text
		cp.Text = &txt
	}
	if part.Media != nil {
		cp.Media = s.deepCloneMediaContent(part.Media)
	}
	return cp
}

// deepCloneMediaContent creates a deep copy of MediaContent including all pointer fields
func (s *ArenaStateStore) deepCloneMediaContent(m *types.MediaContent) *types.MediaContent {
	if m == nil {
		return nil
	}
	cloned := &types.MediaContent{
		MIMEType: m.MIMEType,
	}
	cloned.Data = cloneStringPtr(m.Data)
	cloned.FilePath = cloneStringPtr(m.FilePath)
	cloned.URL = cloneStringPtr(m.URL)
	cloned.StorageReference = cloneStringPtr(m.StorageReference)
	cloned.Format = cloneStringPtr(m.Format)
	cloned.Detail = cloneStringPtr(m.Detail)
	cloned.Caption = cloneStringPtr(m.Caption)
	cloned.PolicyName = cloneStringPtr(m.PolicyName)
	cloned.SizeKB = cloneInt64Ptr(m.SizeKB)
	cloned.Duration = cloneIntPtr(m.Duration)
	cloned.BitRate = cloneIntPtr(m.BitRate)
	cloned.Channels = cloneIntPtr(m.Channels)
	cloned.Width = cloneIntPtr(m.Width)
	cloned.Height = cloneIntPtr(m.Height)
	cloned.FPS = cloneIntPtr(m.FPS)
	return cloned
}

// cloneStringPtr creates a deep copy of a *string
func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// cloneIntPtr creates a deep copy of a *int
func cloneIntPtr(i *int) *int {
	if i == nil {
		return nil
	}
	v := *i
	return &v
}

// cloneInt64Ptr creates a deep copy of a *int64
func cloneInt64Ptr(i *int64) *int64 {
	if i == nil {
		return nil
	}
	v := *i
	return &v
}

// cloneMessageToolCalls clones the ToolCalls slice
func (s *ArenaStateStore) cloneMessageToolCalls(cloned *types.Message, msg *types.Message) {
	if len(msg.ToolCalls) == 0 {
		return
	}

	cloned.ToolCalls = make([]types.MessageToolCall, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		cloned.ToolCalls[i] = types.MessageToolCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: append(json.RawMessage(nil), tc.Args...),
		}
	}
}

// cloneMessageToolResult clones the ToolResult
func (s *ArenaStateStore) cloneMessageToolResult(cloned *types.Message, msg *types.Message) {
	if msg.ToolResult != nil {
		partsCopy := make([]types.ContentPart, len(msg.ToolResult.Parts))
		for i, part := range msg.ToolResult.Parts {
			partsCopy[i] = s.deepCloneContentPart(part)
		}
		cloned.ToolResult = &types.MessageToolResult{
			ID:        msg.ToolResult.ID,
			Name:      msg.ToolResult.Name,
			Parts:     partsCopy,
			Error:     msg.ToolResult.Error,
			LatencyMs: msg.ToolResult.LatencyMs,
		}
	}
}

// cloneMessageCostInfo clones the CostInfo
func (s *ArenaStateStore) cloneMessageCostInfo(cloned *types.Message, msg *types.Message) {
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
}

// cloneMessageMeta clones the Meta map
func (s *ArenaStateStore) cloneMessageMeta(cloned *types.Message, msg *types.Message) {
	if len(msg.Meta) > 0 {
		cloned.Meta = make(map[string]interface{}, len(msg.Meta))
		for k, v := range msg.Meta {
			cloned.Meta[k] = s.deepCloneValue(v)
		}
	}
}

// cloneMessageValidations clones the Validations slice
func (s *ArenaStateStore) cloneMessageValidations(cloned *types.Message, msg *types.Message) {
	if len(msg.Validations) == 0 {
		return
	}

	cloned.Validations = make([]types.ValidationResult, len(msg.Validations))
	for i, vr := range msg.Validations {
		cloned.Validations[i] = types.ValidationResult{
			ValidatorType: vr.ValidatorType,
			Passed:        vr.Passed,
			Timestamp:     vr.Timestamp,
		}
		s.cloneValidationDetails(&cloned.Validations[i], &vr)
	}
}

// cloneValidationDetails clones the Details map in a ValidationResult
func (s *ArenaStateStore) cloneValidationDetails(cloned *types.ValidationResult, vr *types.ValidationResult) {
	if len(vr.Details) > 0 {
		cloned.Details = make(map[string]interface{}, len(vr.Details))
		for k, v := range vr.Details {
			cloned.Details[k] = s.deepCloneValue(v)
		}
	}
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

	// Deep clone the state to prevent callers from mutating internal store data
	stateCopy := s.deepCloneConversationState(&arenaState.ConversationState)

	// Ensure Metadata is never nil to prevent panics
	if stateCopy.Metadata == nil {
		stateCopy.Metadata = make(map[string]interface{})
	}

	return stateCopy, nil
}

// Delete removes conversation state
func (s *ArenaStateStore) Delete(ctx context.Context, conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conversations, conversationID)
	return nil
}

// GetArenaState retrieves the full Arena state including telemetry.
// Returns a deep copy to prevent callers from mutating internal store data.
func (s *ArenaStateStore) GetArenaState(ctx context.Context, conversationID string) (*ArenaConversationState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	arenaState, exists := s.conversations[conversationID]
	if !exists {
		return nil, fmt.Errorf("conversation %s not found", conversationID)
	}

	// Deep clone the state to prevent callers from mutating internal store data
	clonedState := s.deepCloneConversationState(&arenaState.ConversationState)
	result := &ArenaConversationState{
		ConversationState: *clonedState,
	}

	// Deep clone RunMetadata if present
	if arenaState.RunMetadata != nil {
		result.RunMetadata = s.deepCloneRunMetadata(arenaState.RunMetadata)
	}

	return result, nil
}

// deepCloneRunMetadata creates a deep copy of RunMetadata
func (s *ArenaStateStore) deepCloneRunMetadata(m *RunMetadata) *RunMetadata {
	if m == nil {
		return nil
	}
	cloned := &RunMetadata{
		RunID:         m.RunID,
		PromptPack:    m.PromptPack,
		Region:        m.Region,
		ScenarioID:    m.ScenarioID,
		ProviderID:    m.ProviderID,
		StartTime:     m.StartTime,
		EndTime:       m.EndTime,
		Duration:      m.Duration,
		Error:         m.Error,
		SelfPlay:      m.SelfPlay,
		PersonaID:     m.PersonaID,
		RecordingPath: m.RecordingPath,
	}
	cloned.Params = s.deepCloneMap(m.Params)
	cloned.Commit = s.deepCloneMap(m.Commit)
	cloned.TrialResults = m.TrialResults
	s.cloneRunMetadataRoles(cloned, m)
	s.cloneRunMetadataFeedback(cloned, m)
	s.cloneRunMetadataSlices(cloned, m)
	return cloned
}

// deepCloneMap clones a map[string]interface{} deeply
func (s *ArenaStateStore) deepCloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(m))
	for k, v := range m {
		cloned[k] = s.deepCloneValue(v)
	}
	return cloned
}

// cloneRunMetadataRoles clones SelfPlayRoleInfo pointers
func (s *ArenaStateStore) cloneRunMetadataRoles(cloned, m *RunMetadata) {
	if m.AssistantRole != nil {
		ar := *m.AssistantRole
		cloned.AssistantRole = &ar
	}
	if m.UserRole != nil {
		ur := *m.UserRole
		cloned.UserRole = &ur
	}
}

// cloneRunMetadataFeedback clones the UserFeedback field
func (s *ArenaStateStore) cloneRunMetadataFeedback(cloned, m *RunMetadata) {
	if m.UserFeedback == nil {
		return
	}
	fb := *m.UserFeedback
	fb.Categories = s.deepCloneMap(m.UserFeedback.Categories)
	if fb.Tags != nil {
		fb.Tags = make([]string, len(m.UserFeedback.Tags))
		copy(fb.Tags, m.UserFeedback.Tags)
	}
	cloned.UserFeedback = &fb
}

// cloneRunMetadataSlices clones the slice fields in RunMetadata
func (s *ArenaStateStore) cloneRunMetadataSlices(cloned, m *RunMetadata) {
	if m.SessionTags != nil {
		cloned.SessionTags = make([]string, len(m.SessionTags))
		copy(cloned.SessionTags, m.SessionTags)
	}
	if m.ConversationAssertionResults != nil {
		cloned.ConversationAssertionResults = make([]ConversationValidationResult, len(m.ConversationAssertionResults))
		copy(cloned.ConversationAssertionResults, m.ConversationAssertionResults)
	}
	if m.A2AAgents != nil {
		cloned.A2AAgents = make([]A2AAgentInfo, len(m.A2AAgents))
		copy(cloned.A2AAgents, m.A2AAgents)
	}
}

// MediaOutput represents media content produced during a run
type MediaOutput struct {
	Type       string `json:"Type"`       // "image", "audio", "video"
	MIMEType   string `json:"MIMEType"`   // e.g., "image/png", "audio/wav"
	SizeBytes  int64  `json:"SizeBytes"`  // Size in bytes
	Duration   *int   `json:"Duration"`   // Duration in seconds for audio/video
	Width      *int   `json:"Width"`      // Width in pixels for images/video
	Height     *int   `json:"Height"`     // Height in pixels for images/video
	FilePath   string `json:"FilePath"`   // Path to the media file if available
	Thumbnail  string `json:"Thumbnail"`  // Base64-encoded thumbnail for images
	MessageIdx int    `json:"MessageIdx"` // Index of the message containing this media
	PartIdx    int    `json:"PartIdx"`    // Index of the part within the message
}

// RunResult represents the full result structure for JSON compatibility
// This type mirrors engine.RunResult to avoid circular dependencies
type RunResult struct {
	RunID        string                  `json:"RunID"`
	PromptPack   string                  `json:"PromptPack"`
	Region       string                  `json:"Region"`
	ScenarioID   string                  `json:"ScenarioID"`
	ProviderID   string                  `json:"ProviderID"`
	Params       map[string]interface{}  `json:"Params"`
	Messages     []types.Message         `json:"Messages"`
	Commit       map[string]interface{}  `json:"Commit"`
	Cost         types.CostInfo          `json:"Cost"`
	ToolStats    *types.ToolStats        `json:"ToolStats"`
	Violations   []types.ValidationError `json:"Violations"`
	StartTime    time.Time               `json:"StartTime"`
	EndTime      time.Time               `json:"EndTime"`
	Duration     time.Duration           `json:"Duration"`
	Error        string                  `json:"Error"`
	SelfPlay     bool                    `json:"SelfPlay"`
	PersonaID    string                  `json:"PersonaID"`
	MediaOutputs []MediaOutput           `json:"MediaOutputs"` // Media produced during the run

	UserFeedback  *Feedback   `json:"UserFeedback"`
	SessionTags   []string    `json:"SessionTags"`
	AssistantRole interface{} `json:"AssistantRole"` // Using interface{} to avoid circular import
	UserRole      interface{} `json:"UserRole"`

	// Session recording path (if recording is enabled)
	RecordingPath string `json:"RecordingPath,omitempty"`

	// Conversation-level assertions evaluated after the conversation completes (summary format)
	ConversationAssertions AssertionsSummary `json:"conversation_assertions,omitempty"`

	// A2A agent metadata (populated from config for report rendering)
	A2AAgents []A2AAgentInfo `json:"A2AAgents,omitempty"`

	// TrialResults holds aggregated statistics when a scenario is run with Trials > 1.
	TrialResults interface{} `json:"trial_results,omitempty"`
}

// AssertionsSummary mirrors the turn-level assertions structure
type AssertionsSummary struct {
	Failed  int                            `json:"failed"`
	Passed  bool                           `json:"passed"`
	Results []ConversationValidationResult `json:"results"`
	Total   int                            `json:"total"`
}
