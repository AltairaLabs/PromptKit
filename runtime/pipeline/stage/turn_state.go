package stage

import (
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TurnState holds per-Turn invariants shared across stages within a single
// pipeline execution (one Send / one TurnExecutor.ExecuteTurn). It is
// constructed by the pipeline builder and held by reference by every stage
// that needs to read or write per-Turn data.
//
// Per-Turn data lives here, not in StreamElement.Meta, because it is the same
// for every element of the Turn.
//
// # Synchronization
//
// Pipeline stages run as goroutines connected by channels. Writes to
// TurnState happen in pipeline-stage order; the channel hand-off between
// stages provides happens-before, so a downstream reader always observes
// upstream writes by the time the first element arrives.
//
// In practice:
//   - PromptAssemblyStage populates Template, AllowedTools, Validators on
//     its first input element, before forwarding.
//   - VariableProviderStage populates Variables.
//   - TemplateStage reads Template/Variables and writes SystemPrompt.
//   - ProviderStage / ContextBuilderStage / DuplexProviderStage read
//     SystemPrompt and AllowedTools.
//   - Arena instruction stages (skill_instruction, completion_instruction)
//     mutate SystemPrompt; the next stage in pipeline order observes the
//     mutation.
//
// Tests must construct stages in pipeline order or hand-populate fields
// directly when exercising stages in isolation.
type TurnState struct {
	// Template is the unrendered prompt template plus its metadata. Set
	// by PromptAssemblyStage. After population this field is read-only.
	Template *prompt.Template

	// AllowedTools lists tool names the agent may call this Turn. Set by
	// PromptAssemblyStage from the loaded template's AllowedTools.
	AllowedTools []string

	// Validators is the filtered list of pack-declared guardrail
	// configurations active for this Turn (disabled validators removed).
	// Set by PromptAssemblyStage; consumed by Arena's guardrail_eval.
	Validators []prompt.ValidatorConfig

	// Variables is the merged variable map (defaults < fragment vars <
	// explicit). Set by VariableProviderStage / PromptAssemblyStage's
	// merge logic.
	Variables map[string]string

	// SystemPrompt is the rendered system prompt for this Turn. Set by
	// TemplateStage on first execution. Mutable: Arena's instruction
	// stages may append to it between TemplateStage and ProviderStage.
	SystemPrompt string

	// ConversationID identifies the conversation this Turn belongs to.
	// Set by load stages.
	ConversationID string

	// UserID identifies the user owning the conversation, when configured.
	UserID string

	// ProviderRequestMetadata is the per-Turn payload that flows into
	// providers.PredictionRequest.Metadata. Written by upstream stages
	// (mock-scenario, selfplay) and consumed at the provider boundary
	// where typed Arena coordination state must reach a third-party
	// provider whose ABI is itself untyped (map[string]any).
	//
	// Outside that boundary, prefer typed fields. This map exists only
	// because the provider interface is opaque by design. Adding new
	// keys here means you're crossing that boundary.
	//
	// Read-only after the producer stage runs; downstream stages must
	// not mutate it.
	ProviderRequestMetadata map[string]interface{}

	// turnIndex is the 1-based number of the turn being executed, derived from
	// the persisted transcript by whichever load stage opens the turn
	// (StateStoreLoadStage, or ContextAssemblyStage under a context window).
	// Zero means no turn was established — read it through TurnIndex().
	//
	// Atomic, unlike every other field here, because it is the one field
	// written after the stage hand-off that orders the rest. A streaming
	// session runs all its stages concurrently for the whole conversation, so
	// the load stage advances the turn while the provider stage is reading it.
	turnIndex atomic.Int64

	// sessionTurns counts the turns a realtime (duplex) session has completed
	// since it opened. A duplex pipeline runs once for the whole session, so
	// turnIndex holds only the turns persisted before it opened; the provider
	// stage counts the session's own turns here, where the conversation can
	// read them. Always 0 on the text path, where turnIndex advances per turn.
	sessionTurns atomic.Int64

	// callerTurn is the 0-based caller turn of the latest turn a duplex
	// session completed, plus one (0 while none has): see CallerTurn.
	callerTurn atomic.Int64
}

// TurnIndex is the 1-based number of the turn being executed, or 0 when none
// was established (no state store, so no transcript to count).
//
// It is what places an event against the transcript, so everything reporting a
// turn (guardrail events, eval results) must read it from here rather than
// counting locally. A local count is only right while one pipeline instance
// outlives the conversation, and is always wrong for a conversation resumed
// from history it did not itself produce.
func (t *TurnState) TurnIndex() int {
	if t == nil {
		return 0
	}
	return int(t.turnIndex.Load())
}

// SetTurnIndex records the turn being executed. The load stage that opens the
// turn owns this (StateStoreLoadStage or ContextAssemblyStage, via
// deriveTurnIndex); nothing else should write it, or turns get counted twice.
func (t *TurnState) SetTurnIndex(n int) {
	if t != nil {
		t.turnIndex.Store(int64(n))
	}
}

// AdvanceTurn moves to the next turn, returning the new value.
func (t *TurnState) AdvanceTurn() int {
	if t == nil {
		return 0
	}
	return int(t.turnIndex.Add(1))
}

// SessionTurns is the number of turns a realtime session has completed since
// it opened, on top of the TurnIndex it opened at. TurnIndex()+SessionTurns()
// is then the number of turns the conversation has had, on either path: the
// text path never counts session turns, and its TurnIndex is the 1-based
// number of the turn being executed.
func (t *TurnState) SessionTurns() int {
	if t == nil {
		return 0
	}
	return int(t.sessionTurns.Load())
}

// completeSessionTurn counts one completed realtime-session turn. The
// DuplexProviderStage owns this.
func (t *TurnState) completeSessionTurn() {
	if t != nil {
		t.sessionTurns.Add(1)
	}
}

// CallerTurn is the 0-based index of the caller turn the latest completed turn
// of a duplex session answered, the index its guardrails and turn evals
// reported, and whether one has completed. A duplex session's session evals
// report it. The text path never sets it.
func (t *TurnState) CallerTurn() (int, bool) {
	if t == nil {
		return 0, false
	}
	n := t.callerTurn.Load()
	return int(n) - 1, n > 0
}

// setCallerTurn records the caller turn of a turn a duplex session completed.
// The duplex provider stages own this.
func (t *TurnState) setCallerTurn(index int) {
	if t != nil {
		t.callerTurn.Store(int64(index) + 1)
	}
}

// NewTurnState constructs a fresh, empty TurnState.
func NewTurnState() *TurnState {
	return &TurnState{}
}

// BeginTurn clears the per-turn render cache so the next TemplateStage run
// renders again. A caller that reuses one TurnState across turns (the SDK
// Conversation does, to carry the turn index) must call this at the start of
// every turn.
//
// Without it, SystemPrompt stays populated from the first turn and
// renderSystemTemplate returns early forever, so the prompt is rendered once
// per conversation rather than once per turn. Everything that varies between
// turns then silently stops reaching the model: SetVar, per-send bindings,
// dynamic variable providers, and retrieved memory context.
//
// It also clears Variables, which VariableProviderStage fills from the
// turn's own sources. Without that, a per-send value (a send-scoped binding,
// WithJSONInput in the SDK) stayed on every later turn, where it rendered and
// satisfied required variables. Set any variables of your own after calling
// it.
//
// It leaves the turn's other fields alone. Template, AllowedTools
// and Validators are repopulated by PromptAssemblyStage on each run, and the
// turn index belongs to the load stage.
func (t *TurnState) BeginTurn() {
	if t == nil {
		return
	}
	t.SystemPrompt = ""
	t.Variables = nil
}

// ElementMetadata is the typed schema for per-element coordination data.
// Unlike TurnState (which is per-Turn-invariant), fields here genuinely
// differ between elements within the same Turn.
//
// Only data that differs between elements of the same Turn belongs here.
type ElementMetadata struct {
	// FromHistory marks elements emitted by load stages that represent
	// historical messages (vs the current input element of this Turn).
	// Read by stages that need to skip per-Turn work for historical
	// elements (e.g. TemplateStage does not render for history rows).
	FromHistory bool

	// MergeInputIndex records which input channel this element arrived on
	// when forwarded by a MergeStage. Nil for elements not produced by a
	// merge stage.
	MergeInputIndex *int

	// ContextTruncated marks elements where the upstream context-window
	// stage truncated history before assembling this Turn's prompt.
	ContextTruncated bool

	// EnableCacheBreakpoints marks elements that should cause downstream
	// providers to insert prompt-cache breakpoints.
	EnableCacheBreakpoints bool

	// TurnID identifies the user turn this element belongs to in duplex
	// flows. Producers (Arena duplex executor for user-message elements;
	// DuplexProviderStage for the EOS element that closes a transcribed
	// turn) set it; downstream stages pair turns by reading it.
	TurnID *string

	// Transcription carries duplex-provider input transcription data
	// attached to the element that closed a user turn. Nil for elements
	// without transcription.
	Transcription *Transcription

	// AllResponsesReceived signals (on a marker element from the
	// duplex executor) that the duplex provider has emitted every
	// response for the active turn.
	AllResponsesReceived bool

	// Interrupted marks elements emitted to indicate that a duplex
	// session was interrupted mid-response.
	Interrupted bool

	// ReplacesText marks a text element that replaces all the text the turn
	// streamed before it: an output guardrail cut the reply, and the element's
	// text is its message, the turn's whole text. A consumer rendering deltas
	// swaps what it showed for it (providers.StreamChunk.ReplacesText).
	ReplacesText bool

	// SegmentEnd marks a text delta whose text ends a segment of the reply
	// (a sentence or a few), or the reply itself, as a stream_hold releases
	// it: the provider stage has released the segment whole, so a consumer
	// that works a segment at a time (speech output) can take it at once,
	// without waiting for the text after it.
	SegmentEnd bool

	// RoundTextEnd marks a control element the provider stage sends, on a
	// streaming (duplex) turn, when a round's stream has finished and the
	// round goes on to run its tool calls: the round's text (MessageID) is
	// complete. A speech-out stage speaks what it holds of it at once, so a
	// preamble ("Let me look that up.") is heard while the tools run. The
	// element carries nothing else and is not output for the caller.
	RoundTextEnd bool

	// MessageID is the stable id (types.MetaMessageID) of the assistant
	// message a text element is part of: the round's reply, which is stored
	// under it, so a consumer streaming the text can name the message as the
	// history will (providers.StreamChunk.MessageID).
	MessageID string

	// InterruptedTurnComplete marks the element that closes an
	// interrupted turn (downstream cleanup signal).
	InterruptedTurnComplete bool

	// LateTranscript marks a user Message that DuplexProviderStage
	// materialized from a provider's final input transcript after the reply
	// to it had completed (OpenAI Realtime delivers it late). It is the
	// answered turn's user message, never the next turn's: a
	// TranscriptReorderStage still holding that turn places it ahead of the
	// reply, and one that has already resolved the turn drops it.
	LateTranscript bool

	// ToolCallsRunning marks the turn-end element of a model response whose
	// tool calls DuplexProviderStage runs itself: each call's result (or a
	// PendingTools element for a call held for the caller) follows on a later
	// element. TranscriptReorderStage does not time out a held turn while
	// such a round is still running.
	ToolCallsRunning bool

	// PlayoutHeld marks an element whose Message PlayoutHoldStage is holding
	// until the reply's audio has played (or been cut): the transcript stages
	// (recording, message broadcast, memory, save) skip that Message and take
	// it from the PlayoutRelease element that follows. The element itself
	// still reaches the caller at once.
	PlayoutHeld bool

	// PlayoutRelease marks an element PlayoutHoldStage emits to release a held
	// Message into the transcript, amended when the caller heard only part of
	// the reply. It is for the transcript stages only: it is not output for
	// the caller, and it is never a turn end (EndOfStream): the held element
	// carried the turn end downstream when it happened, so each reply's turn
	// ends once for every stage.
	PlayoutRelease bool

	// CallerTurn, on the element that ends a reply (or a blocked turn's
	// message), is the 0-based index of the caller turn it answers: the turn
	// index the reply's guardrails reported, and its turn evals report.
	CallerTurn *int

	// AwaitsTranscript, with CallerTurn, records that the caller turn was
	// spoken and its transcript has not arrived: a provider that delivers it
	// late sends it after the reply (LateTranscript).
	AwaitsTranscript bool

	// PlayoutTurnEnd, on a PlayoutRelease element, records that the held
	// element was a turn end (EndOfStream, or the Interrupted element carrying
	// a reply barge-in cut short), for a stage that treats a turn-end Message
	// differently from others.
	PlayoutTurnEnd bool

	// CueAudio marks audio a cue stage played for a cue, not reply audio:
	// playout accounting keeps it out of the reply's audio, and a flush-on-cut
	// AudioPacingStage drops a preemptible cue's queued audio when reply audio
	// arrives.
	CueAudio *Cue

	// ToolResultMessages carries tool-execution result messages produced
	// by the streaming tool integration; the duplex provider stage
	// consumes them as the next-round input.
	ToolResultMessages []types.Message

	// ToolResponses carries provider-bound tool call responses produced
	// by the streaming tool integration. The duplex provider stage
	// forwards them to a session that implements ToolResponseSupport.
	ToolResponses []providers.ToolResponse

	// PendingTools carries tool calls that returned ToolStatusPending and
	// must be surfaced to the caller for out-of-band completion.
	PendingTools []tools.PendingToolExecution

	// RoundMessages reports a tool round of a streamed turn as it happens:
	// the assistant message that made tool calls, once its reply has streamed
	// and before the calls run, then the tool results the round fed back to
	// the model, once they ran. The element carries no Message: the turn's
	// messages still reach the transcript stages as Message elements when the
	// turn ends, so a stage that records the transcript ignores it. It is for a
	// caller that shows the turn while it runs (providers.StreamChunk).
	RoundMessages []types.Message

	// ProviderCallEnd marks the end of one provider call's stream in a
	// per-turn streamed execution: it follows the call's last delta and comes
	// before its tool calls run, and every call of the turn sends one, the
	// last included. Like RoundMessages, the element carries no Message and
	// is for a caller that shows the turn while it runs. The continuous
	// (duplex) mode does not send it.
	ProviderCallEnd *providers.ProviderCallEnd

	// SynthesizedSpeech marks an element produced by a TTS stage: it carries the
	// spoken audio plus, for reference, the text that was submitted for synthesis.
	// That text is the same reply already delivered as streaming Text deltas, so a
	// consumer converting elements to output chunks must not re-present it as new
	// model output (Delta). The audio is the payload. The text is also not
	// necessarily the spoken text, since providers strip or rewrite markup at
	// synthesis.
	SynthesizedSpeech bool

	// SpokenText is the text the TTS provider actually spoke, after markup
	// lowering. It is the provider- and model-specific value that the reference
	// Text only approximates. Populated on SynthesizedSpeech elements when the
	// provider implements tts.SpokenTextReporter; empty otherwise, and consumers
	// then fall back to the reference text.
	SpokenText string

	// StreamingDelta marks an incremental content Text element emitted by a
	// streaming provider mid-generation (see ProviderStage.emitChunkElement).
	// These are the live-text feed for UI consumers, and the text the provider
	// stage has released to the caller (output guardrails have cleared it); the
	// same reply also arrives as a complete assistant Message. A speech-out
	// stage speaks either the deltas, gathered into sentence-sized segments
	// (TTSStageWithInterruption: one request per token would be choppy audio),
	// or the Message (TTSStage), never both.
	StreamingDelta bool

	// BargeInIfTranscribed marks a caller turn's audio that ended while the
	// bot was engaged in a reply but that the VAD never reported as speech
	// (Speaking): a short word, or a cough or a door. AudioTurnStage leaves
	// the reply alone; the STT stage interrupts it once the turn transcribes
	// to words, so noise never cuts a reply off.
	BargeInIfTranscribed bool

	// TokenCount is the per-chunk token count emitted by streaming providers.
	// Zero when the chunk did not include a token count.
	TokenCount int

	// FinishReason carries the provider-reported stop reason on the chunk
	// element that closes a streaming response (e.g. "stop", "length",
	// "tool_calls"). Nil for intermediate chunks.
	FinishReason *string

	// Passthrough marks media elements that should bypass per-stage
	// accumulation (audio/image/video buffering) and be forwarded
	// immediately to the next stage. streaming.ImageStreamer and
	// streaming.VideoStreamer set it on the elements they send.
	// AudioTurnStage forwards passthrough audio without VAD-driven
	// accumulation.
	Passthrough bool

	// MediaExtract carries correlation data for the MediaExtractStage →
	// MediaComposeStage round-trip. Nil for elements not produced by the
	// extract stage.
	MediaExtract *MediaExtractInfo

	// VideoFrames carries correlation data for the VideoFramesExtractStage
	// → VideoFramesComposeStage round-trip. Nil for elements not produced
	// by the frames extractor.
	VideoFrames *VideoFramesInfo
}

// MediaExtractInfo is the typed payload for media-extraction correlation.
type MediaExtractInfo struct {
	// MessageID identifies the original message the extracted part belongs to.
	MessageID string
	// PartIndex is the part's index within the original message.
	PartIndex int
	// TotalParts is the total number of media parts in the original message.
	TotalParts int
	// MediaType (image, video) classifies the extracted part.
	MediaType string
	// OriginalMessage is the full source message for later composition.
	OriginalMessage *types.Message
}

// VideoFramesInfo is the typed payload for video-frame extraction correlation.
type VideoFramesInfo struct {
	// VideoID identifies the source video an extracted frame belongs to.
	VideoID string
	// FrameIndex is the frame's index within the source video.
	FrameIndex int
	// TotalFrames is the total number of frames extracted from the source.
	TotalFrames int
	// OriginalVideo is the source video kept for later composition.
	OriginalVideo *VideoData
}

// Transcription is the typed payload for duplex-provider input
// transcription attached to an element via ElementMetadata.
type Transcription struct {
	// Text is the transcribed user audio.
	Text string
}
