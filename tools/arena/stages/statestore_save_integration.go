package stages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	runtimeStatestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

const (
	contentPreviewMaxLen = 40 // Maximum length for content preview in logs
)

// ArenaStateStoreSaveStage saves conversation state with telemetry to ArenaStateStore.
// This stage captures validation results, turn metrics, and cost information
// for Arena testing and analysis.
type ArenaStateStoreSaveStage struct {
	stage.BaseStage
	config    *pipeline.StateStoreConfig
	turnState *stage.TurnState
	emitter   *events.Emitter
	// baseTranscriptLen is the persisted transcript length captured at build
	// time (in WithEmitter, before the pipeline runs). Live broadcasts use it
	// as the transcript-absolute base for this turn's new messages. Capturing
	// at build time is race-free: the runtime ProviderStage write-through that
	// appends this turn's messages only runs once the pipeline executes, so a
	// read inside Process would race it and miscount.
	baseTranscriptLen int
}

// NewArenaStateStoreSaveStage creates a new Arena state store save stage.
func NewArenaStateStoreSaveStage(config *pipeline.StateStoreConfig) *ArenaStateStoreSaveStage {
	return NewArenaStateStoreSaveStageWithTurnState(config, nil)
}

// NewArenaStateStoreSaveStageWithTurnState creates an Arena state store
// save stage that reads the rendered system prompt from the supplied
// TurnState. Falls back to config.Metadata["system_prompt"] when the
// TurnState is empty (used by tests that wire the prompt via config).
func NewArenaStateStoreSaveStageWithTurnState(
	config *pipeline.StateStoreConfig, turnState *stage.TurnState,
) *ArenaStateStoreSaveStage {
	return &ArenaStateStoreSaveStage{
		BaseStage: stage.NewBaseStage("arena_statestore_save", stage.StageTypeSink),
		config:    config,
		turnState: turnState,
	}
}

// WithEmitter wires an events.Emitter so the stage broadcasts each message
// to the event bus the moment it arrives. Live UIs (TUI conversation panel,
// web SSE relay) can then render turns as they happen instead of waiting
// for run completion. Stage-level state save remains the source of truth
// for replay and post-run results.
func (s *ArenaStateStoreSaveStage) WithEmitter(emitter *events.Emitter) *ArenaStateStoreSaveStage {
	s.emitter = emitter
	s.baseTranscriptLen = s.captureBaseTranscriptLen()
	return s
}

// captureBaseTranscriptLen reads the already-persisted transcript length for
// this conversation. Called synchronously from WithEmitter at pipeline-build
// time — before Execute — so it sees only prior turns, never this turn's own
// write-through. Returns 0 when no store is wired or the load fails (a fresh
// conversation reads as 0). Used as the transcript-absolute base for live
// message broadcasts; the persisted state stays the source of truth.
func (s *ArenaStateStoreSaveStage) captureBaseTranscriptLen() int {
	if s.config == nil || s.config.Store == nil {
		return 0
	}
	store, ok := s.config.Store.(arenaSaveStore)
	if !ok {
		return 0
	}
	state, err := store.Load(context.Background(), s.config.ConversationID)
	if err != nil || state == nil {
		return 0
	}
	return len(state.Messages)
}

// collectedData holds all data collected from stream elements.
type collectedData struct {
	messages              []types.Message
	metadata              map[string]interface{}
	trace                 *pipeline.ExecutionTrace
	costInfo              *types.CostInfo
	transcriptionsApplied int // Tracks how many transcriptions have been applied (for ordering)
}

// collectFromElement extracts data from a single element and updates collected data.
func (d *collectedData) collectFromElement(elem *stage.StreamElement) {
	if elem.Message != nil {
		contentPreview := elem.Message.Content
		if len(contentPreview) > contentPreviewMaxLen {
			contentPreview = contentPreview[:contentPreviewMaxLen] + "..."
		}
		logger.Debug("ArenaStateStoreSaveStage: collecting message",
			"role", elem.Message.Role,
			"content_len", len(elem.Message.Content),
			"content_preview", contentPreview,
			"total_messages", len(d.messages)+1)
		d.messages = append(d.messages, *elem.Message)
		if elem.Message.CostInfo != nil {
			d.costInfo = elem.Message.CostInfo
		}
	}

	// Apply duplex input transcription to the correct user message. When a
	// turn id is set we pair by turn id; otherwise fall back to the order
	// in which transcriptions arrive.
	if elem.Meta.Transcription != nil && elem.Meta.Transcription.Text != "" {
		if elem.Meta.TurnID != nil && *elem.Meta.TurnID != "" {
			d.applyTranscriptionByTurnID(elem.Meta.Transcription.Text, *elem.Meta.TurnID)
		} else {
			d.applyNextTranscription(elem.Meta.Transcription.Text)
		}
	}
}

// applyTranscriptionByTurnID applies a transcription to the user message with matching turn_id.
// This provides reliable correlation between audio input and its transcription,
// regardless of message arrival order.
//
// For selfplay messages (those with meta.self_play == true), the original LLM-generated
// text is preserved in meta.original_text before being replaced by the transcription.
func (d *collectedData) applyTranscriptionByTurnID(transcript, turnID string) {
	for i := 0; i < len(d.messages); i++ {
		if d.messages[i].Role == roleUser {
			// Check if this message has the matching turn_id
			if d.messages[i].Meta != nil {
				if msgTurnID, ok := d.messages[i].Meta["turn_id"].(string); ok && msgTurnID == turnID {
					d.applyTranscriptionToMessage(i, transcript)
					logger.Debug("ArenaStateStoreSaveStage: applied transcription by turn_id",
						"message_index", i,
						"turn_id", turnID,
						"transcription_len", len(transcript))
					return
				}
			}
		}
	}
	logger.Warn("ArenaStateStoreSaveStage: no user message found with turn_id",
		"turn_id", turnID,
		"total_messages", len(d.messages))
}

// applyNextTranscription applies a transcription to the N-th user message in order.
// This is the fallback method when turn_id is not available.
// Transcriptions arrive in turn order:
// - First transcription is for the first user turn
// - Second transcription is for the second user turn
// - etc.
//
// We use the transcriptionsApplied counter to find the correct user message,
// which handles race conditions where messages may arrive out of collection order.
//
// For selfplay messages (those with meta.self_play == true), the original LLM-generated
// text is preserved in meta.original_text before being replaced by the transcription.
// This allows comparing what the selfplay LLM generated vs what Gemini heard.
func (d *collectedData) applyNextTranscription(transcript string) {
	// Find the N-th user message (where N = transcriptionsApplied)
	userCount := 0
	for i := 0; i < len(d.messages); i++ {
		if d.messages[i].Role == roleUser {
			if userCount == d.transcriptionsApplied {
				// This is the user message that should receive this transcription
				d.applyTranscriptionToMessage(i, transcript)
				d.transcriptionsApplied++
				logger.Debug("ArenaStateStoreSaveStage: applied input transcription to user message",
					"message_index", i,
					"transcription_number", d.transcriptionsApplied,
					"transcription_len", len(transcript))
				return
			}
			userCount++
		}
	}
	logger.Warn("ArenaStateStoreSaveStage: no user message found to apply transcription",
		"transcription_number", d.transcriptionsApplied+1,
		"user_messages_found", userCount,
		"total_messages", len(d.messages))
}

// applyTranscriptionToMessage applies a transcription to a specific message by index.
func (d *collectedData) applyTranscriptionToMessage(msgIdx int, transcript string) {
	// For selfplay messages, preserve the original generated text in meta
	// before overwriting with transcription
	if d.messages[msgIdx].Meta != nil {
		if _, isSelfplay := d.messages[msgIdx].Meta["self_play"].(bool); isSelfplay {
			// Store original text if not already stored
			if _, hasOriginal := d.messages[msgIdx].Meta["original_text"]; !hasOriginal {
				d.messages[msgIdx].Meta["original_text"] = d.messages[msgIdx].Content
			}
		}
	}

	// Update the Content field with the transcription
	d.messages[msgIdx].Content = transcript

	// Also add/update a text part with the transcription
	textFound := false
	for j := range d.messages[msgIdx].Parts {
		if d.messages[msgIdx].Parts[j].Type == types.ContentTypeText || d.messages[msgIdx].Parts[j].Text != nil {
			d.messages[msgIdx].Parts[j].Text = &transcript
			d.messages[msgIdx].Parts[j].Type = types.ContentTypeText
			textFound = true
			break
		}
	}
	// If no text part exists, prepend one
	if !textFound {
		newPart := types.ContentPart{
			Type: types.ContentTypeText,
			Text: &transcript,
		}
		d.messages[msgIdx].Parts = append([]types.ContentPart{newPart}, d.messages[msgIdx].Parts...)
	}
}

// forwardElements just forwards elements without collecting.
func forwardElements(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// arenaSaveStore is the subset of *statestore.ArenaStateStore that this
// stage needs (Load + SaveWithTrace). Defined locally so tests can supply
// a counting wrapper to verify that Load is called once per Process call,
// not once per persisted Message.
type arenaSaveStore interface {
	Load(ctx context.Context, id string) (*runtimeStatestore.ConversationState, error)
	SaveWithTrace(
		ctx context.Context,
		state *runtimeStatestore.ConversationState,
		trace *pipeline.ExecutionTrace,
	) error
}

// Process collects messages and saves them incrementally to Arena state store.
// Messages are saved after each turn completion (when an element contains a Message).
// This ensures conversation state is captured in real-time as turns complete.
//
// The conversation state is loaded lazily on the first save and reused for
// subsequent saves within the same Process call — avoiding O(N) Loads on
// scenarios with many Message elements.
func (s *ArenaStateStoreSaveStage) Process(
	ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement,
) error {
	defer close(output)

	if s.config == nil || s.config.Store == nil {
		return forwardElements(ctx, input, output)
	}

	arenaStore, ok := s.config.Store.(arenaSaveStore)
	if !ok {
		//nolint:lll // Error message with type name - acceptable long line
		return fmt.Errorf("arena state store save: invalid store type, expected arenaSaveStore (e.g. *statestore.ArenaStateStore), got %T", s.config.Store)
	}

	data := &collectedData{}
	var cachedState *runtimeStatestore.ConversationState
	ctxCanceled := false

	// prevLen is the pre-turn transcript length captured at build time (see
	// captureBaseTranscriptLen) — the count of messages the element stream
	// replays before this turn's new ones. Elements at a stream position below
	// it are history re-plays and must NOT be re-emitted (no re-fire of prior
	// turns). Reading it here from the store would race the ProviderStage
	// write-through that appends this turn's messages mid-stream.
	prevLen := s.baseTranscriptLen

	// The system prompt / offset are resolved lazily on the first element:
	// the rendered system prompt is set by the upstream TemplateStage, which
	// runs concurrently with this sink — at Process start it may not be ready.
	// systemOffset accounts for a system message persistState will prepend
	// that the element stream does not replay (first turn only).
	var systemOffset int
	broadcastReady := s.emitter == nil

	for elem := range input {
		if !broadcastReady {
			s.broadcastSystemPromptIfNeeded(prevLen)
			systemOffset = s.systemBroadcastOffset(prevLen)
			broadcastReady = true
		}
		data.collectFromElement(&elem)
		pos := len(data.messages) - 1
		if elem.Message != nil && pos >= prevLen {
			s.broadcastMessage(&elem, pos+systemOffset)
		}
		cachedState = s.maybeIncrementalSave(ctx, arenaStore, &elem, data, cachedState, ctxCanceled)

		// Try to forward element, but if context is canceled, just drain remaining input
		// so we still capture trailing messages (including partial responses) on timeout.
		select {
		case output <- elem:
		case <-ctx.Done():
			if !ctxCanceled {
				logger.Debug("ArenaStateStoreSaveStage: context canceled, will drain remaining elements")
				ctxCanceled = true
			}
		}
	}

	logger.Debug("ArenaStateStoreSaveStage: final save",
		"message_count", len(data.messages))

	saveCtx := ctxOrBackground(ctx, ctxCanceled)
	loaded, err := s.ensureLoaded(saveCtx, arenaStore, cachedState)
	if err != nil {
		return fmt.Errorf("arena state store save: %w", err)
	}
	cachedState = loaded
	if err := s.persistState(
		saveCtx, arenaStore, cachedState,
		data.messages, data.metadata, data.trace, data.costInfo,
	); err != nil {
		return fmt.Errorf("arena state store save: %w", err)
	}

	return nil
}

// broadcastSystemPromptIfNeeded emits a synthetic system MessageCreated event
// once, on the first turn of a conversation, when a rendered system prompt is
// available. Without this the live UI never sees the system turn until the run
// completes and the saved result is refetched — confusing during a demo.
//
// "First turn" is detected from prevLen (the pre-turn transcript length
// captured race-free at build time): prevLen == 0 means no prior messages,
// hence no system message has been emitted yet. Re-loading the store here to
// count messages would race the ProviderStage write-through and intermittently
// suppress or double-fire the system turn. Called on the first element so the
// rendered system prompt (set by the concurrent TemplateStage) is ready.
func (s *ArenaStateStoreSaveStage) broadcastSystemPromptIfNeeded(prevLen int) {
	if s.emitter == nil || prevLen > 0 {
		return
	}
	sp := s.resolveSystemPrompt()
	if sp == "" {
		return
	}
	s.emitter.MessageCreated(roleSystem, sp, 0, nil, nil, nil)
}

// resolveSystemPrompt returns the rendered system prompt for this turn,
// sourced from TurnState (set by TemplateStage) with a fallback to config
// metadata for tests that wire the prompt through config. Mirrors the
// resolution in persistState so broadcasts and persistence agree.
func (s *ArenaStateStoreSaveStage) resolveSystemPrompt() string {
	sp := ""
	if s.turnState != nil {
		sp = s.turnState.SystemPrompt
	}
	if sp == "" && s.config != nil && s.config.Metadata != nil {
		if v, ok := s.config.Metadata["system_prompt"].(string); ok {
			sp = v
		}
	}
	return sp
}

// systemBroadcastOffset returns 1 when a system message will be prepended on
// persist but is absent from both the pre-turn transcript and the replayed
// element stream (the first turn of a conversation); otherwise 0. Adding it
// keeps live indices aligned with the persisted transcript where index 0 is
// the system prompt. prevLen is the pre-turn transcript length: when it is 0
// the conversation has no persisted (hence no system) messages yet.
func (s *ArenaStateStoreSaveStage) systemBroadcastOffset(prevLen int) int {
	if s.emitter == nil {
		return 0
	}
	if prevLen == 0 && s.resolveSystemPrompt() != "" {
		return 1
	}
	return 0
}

// broadcastMessage publishes the just-arrived message to the event bus when
// an emitter is configured. Skipped when the element carries no Message, when
// the index is out of range, or when the message is the system prompt (system
// is broadcast once by broadcastSystemPromptIfNeeded). idx is the message's
// transcript-absolute position. Live UI consumers (TUI, web SSE) subscribe to
// the bus and update their views in real time.
func (s *ArenaStateStoreSaveStage) broadcastMessage(elem *stage.StreamElement, idx int) {
	if s.emitter == nil || elem.Message == nil || idx < 0 || elem.Message.Role == roleSystem {
		return
	}
	s.emitter.MessageCreated(
		elem.Message.Role,
		elem.Message.Content,
		idx,
		elem.Message.Parts,
		convertToolCalls(elem.Message.ToolCalls),
		convertToolResult(elem.Message.ToolResult),
	)
}

// convertToolCalls converts runtime tool calls to the wire-shaped events
// type. Without this, assistant turns whose content IS a tool invocation
// (i.e., empty Content + populated ToolCalls) arrive in the live UI as
// empty bubbles — the conversation looks like turns are missing.
func convertToolCalls(in []types.MessageToolCall) []events.MessageToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]events.MessageToolCall, len(in))
	for i, c := range in {
		out[i] = events.MessageToolCall{
			ID:   c.ID,
			Name: c.Name,
			Args: string(c.Args),
		}
	}
	return out
}

// convertToolResult converts a runtime tool result to the wire-shaped events
// type. Tool result messages with no top-level Content are otherwise rendered
// as blank turns in the live UI — the actual result lives in Parts/Error.
func convertToolResult(in *types.MessageToolResult) *events.MessageToolResult {
	if in == nil {
		return nil
	}
	return &events.MessageToolResult{
		ID:        in.ID,
		Name:      in.Name,
		Parts:     in.Parts,
		Error:     in.Error,
		LatencyMs: in.LatencyMs,
	}
}

// ctxOrBackground returns context.Background when the upstream context has
// been canceled, so save calls still complete after a pipeline cancellation.
// NOSONAR: Intentional background context — ensures data persistence on cancellation.
func ctxOrBackground(ctx context.Context, ctxCanceled bool) context.Context {
	if ctxCanceled {
		return context.Background()
	}
	return ctx
}

// maybeIncrementalSave persists the in-flight state when the latest element
// completes a turn (carries a Message). Loads lazily on the first save and
// reuses the cached pointer for subsequent calls within the same Process.
// Returns the cached state unchanged when the element has no Message.
func (s *ArenaStateStoreSaveStage) maybeIncrementalSave(
	ctx context.Context,
	arenaStore arenaSaveStore,
	elem *stage.StreamElement,
	data *collectedData,
	cachedState *runtimeStatestore.ConversationState,
	ctxCanceled bool,
) *runtimeStatestore.ConversationState {
	if elem.Message == nil {
		return cachedState
	}

	logger.Debug("ArenaStateStoreSaveStage: saving after turn completion",
		"message_count", len(data.messages),
		"last_role", elem.Message.Role,
		"last_content_len", len(elem.Message.Content))

	saveCtx := ctxOrBackground(ctx, ctxCanceled)
	loaded, err := s.ensureLoaded(saveCtx, arenaStore, cachedState)
	if err != nil {
		logger.Error("ArenaStateStoreSaveStage: failed to load state for incremental save",
			"error", err)
		return cachedState
	}
	if saveErr := s.persistState(
		saveCtx, arenaStore, loaded,
		data.messages, data.metadata, data.trace, data.costInfo,
	); saveErr != nil {
		logger.Error("ArenaStateStoreSaveStage: failed to save after turn", "error", saveErr)
	}
	return loaded
}

// ensureLoaded returns the cached state if non-nil, otherwise Loads from
// the store (creating a fresh state if the conversation doesn't exist).
// Called once per Process invocation in the typical case.
func (s *ArenaStateStoreSaveStage) ensureLoaded(
	ctx context.Context, arenaStore arenaSaveStore, cached *runtimeStatestore.ConversationState,
) (*runtimeStatestore.ConversationState, error) {
	if cached != nil {
		return cached, nil
	}
	state, err := arenaStore.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}
	if state == nil {
		state = s.createNewState()
	}
	return state, nil
}

// createNewState creates a new conversation state with config defaults.
func (s *ArenaStateStoreSaveStage) createNewState() *runtimeStatestore.ConversationState {
	state := &runtimeStatestore.ConversationState{
		ID:       s.config.ConversationID,
		UserID:   s.config.UserID,
		Messages: make([]types.Message, 0),
		Metadata: make(map[string]interface{}),
	}
	for k, v := range s.config.Metadata {
		state.Metadata[k] = v
	}
	return state
}

// updateStateMetadata updates state metadata with execution data and cost info.
func updateStateMetadata(
	state *runtimeStatestore.ConversationState,
	metadata map[string]interface{},
	costInfo *types.CostInfo,
) {
	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}
	for k, v := range metadata {
		state.Metadata[k] = v
	}
	if costInfo != nil && costInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = costInfo.TotalCost
		state.Metadata["total_tokens"] = costInfo.InputTokens + costInfo.OutputTokens
	}
}

// ensureTrace returns the provided trace or creates a default one.
func ensureTrace(trace *pipeline.ExecutionTrace) *pipeline.ExecutionTrace {
	if trace != nil {
		return trace
	}
	return &pipeline.ExecutionTrace{
		StartedAt: time.Now(),
		LLMCalls:  []pipeline.LLMCall{},
		Events:    []pipeline.TraceEvent{},
	}
}

// persistState writes the supplied state with telemetry, mutating it in
// place to reflect the latest collected messages, metadata, and cost info.
// Caller is responsible for loading the state once (via ensureLoaded) and
// passing the same pointer back on subsequent calls within a Process
// invocation — this is what avoids redundant Loads.
//
// When the store implements runtimeStatestore.MessageLog, messages were
// already persisted per-round by the ProviderStage write-through. In that
// case we read the authoritative transcript from the log instead of using
// the pipeline-stream-collected slice (which may be incomplete on the error
// path). This prevents both double-writing on success and message loss on
// mid-loop errors.
func (s *ArenaStateStoreSaveStage) persistState(
	ctx context.Context,
	arenaStore arenaSaveStore,
	state *runtimeStatestore.ConversationState,
	messages []types.Message,
	metadata map[string]interface{},
	trace *pipeline.ExecutionTrace,
	costInfo *types.CostInfo,
) error {
	// Source the system prompt from TurnState (set by TemplateStage), then
	// fall back to config metadata for tests that wire SystemPrompt through
	// the config path.
	systemPrompt := ""
	if s.turnState != nil {
		systemPrompt = s.turnState.SystemPrompt
	}
	if systemPrompt == "" && s.config != nil && s.config.Metadata != nil {
		if sp, ok := s.config.Metadata["system_prompt"].(string); ok && sp != "" {
			systemPrompt = sp
		}
	}

	// On the error path the ProviderStage emits only an error element — no
	// response messages reach this stage's input channel, so messages is empty.
	// If the store implements MessageLog the ProviderStage wrote per-round
	// messages via LogAppend+preSeedLog (including history). Do a fresh Load()
	// to recover the full transcript.
	//
	// On the success path messages already contains everything the pipeline
	// emitted (including assertion-decorated messages from downstream stages);
	// we must NOT replace it with a stale store snapshot that predates those
	// decorations. The guard len(messages)==0 limits the recovery to the error
	// path only.
	if len(messages) == 0 && s.config != nil {
		if _, ok := s.config.Store.(runtimeStatestore.MessageLog); ok {
			freshState, freshErr := arenaStore.Load(ctx, s.config.ConversationID)
			if freshErr == nil && freshState != nil && len(freshState.Messages) > 0 {
				messages = freshState.Messages
			}
		}
	}

	if systemPrompt != "" {
		state.Messages = prependSystemMessage(messages, systemPrompt)
	} else {
		state.Messages = make([]types.Message, len(messages))
		copy(state.Messages, messages)
	}

	// Merge metadata from collected sources and TurnState provider-request
	// metadata so per-Turn coordination data (e.g. arena turn counters)
	// reaches the persisted state.
	mergedMetadata := mergeMetadata(metadata, s.turnStateMetadata())
	updateStateMetadata(state, mergedMetadata, costInfo)

	if err := arenaStore.SaveWithTrace(ctx, state, ensureTrace(trace)); err != nil {
		return fmt.Errorf("failed to save with trace: %w", err)
	}

	return nil
}

// turnStateMetadata returns the TurnState's ProviderRequestMetadata, or nil
// when no TurnState is wired.
func (s *ArenaStateStoreSaveStage) turnStateMetadata() map[string]interface{} {
	if s.turnState == nil {
		return nil
	}
	return s.turnState.ProviderRequestMetadata
}

// mergeMetadata returns a single map with all keys from a then b. b wins on
// collisions. nil inputs are tolerated.
func mergeMetadata(a, b map[string]interface{}) map[string]interface{} {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(a)+len(b))
	for k, v := range a {
		merged[k] = v
	}
	for k, v := range b {
		merged[k] = v
	}
	return merged
}

// createSystemMessage creates a system message with the given prompt and timestamp.
func createSystemMessage(systemPrompt string, timestamp time.Time) types.Message {
	textContent := systemPrompt
	return types.Message{
		Role:    roleSystem,
		Content: systemPrompt,
		Parts: []types.ContentPart{
			{
				Type: "text",
				Text: &textContent,
			},
		},
		Timestamp: timestamp,
	}
}

// prependSystemMessage prepends a system message if not already present.
func prependSystemMessage(messages []types.Message, systemPrompt string) []types.Message {
	// Check if first message is already a system message
	if len(messages) > 0 && messages[0].Role == roleSystem {
		// Already has system message, return as-is
		result := make([]types.Message, len(messages))
		copy(result, messages)
		return result
	}

	// Determine timestamp for system message
	var timestamp time.Time
	if len(messages) > 0 {
		timestamp = messages[0].Timestamp
	} else {
		timestamp = time.Now()
	}

	// Create new slice with system message prepended
	systemMsg := createSystemMessage(systemPrompt, timestamp)
	result := make([]types.Message, 0, len(messages)+1)
	result = append(result, systemMsg)
	result = append(result, messages...)
	return result
}
