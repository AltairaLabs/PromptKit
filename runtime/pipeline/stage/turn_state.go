package stage

import (
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TurnState holds per-Turn invariants shared across stages within a single
// pipeline execution (one Send / one TurnExecutor.ExecuteTurn). It is
// constructed by the pipeline builder and held by reference by every stage
// that needs to read or write per-Turn data.
//
// See `runtime/pipeline/stage/ARCHITECTURE.md` §4 for the data-flow
// principle and the rationale for moving per-Turn data out of
// `StreamElement.Metadata` (issue #1035).
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
	// because the provider interface is opaque by design — adding new
	// keys here means you're crossing that boundary.
	//
	// Read-only after the producer stage runs; downstream stages must
	// not mutate it.
	ProviderRequestMetadata map[string]interface{}
}

// NewTurnState constructs a fresh, empty TurnState.
func NewTurnState() *TurnState {
	return &TurnState{}
}

// ElementMetadata is the typed schema for per-element coordination data.
// Unlike TurnState (which is per-Turn-invariant), fields here genuinely
// differ between elements within the same Turn.
//
// The current set is small because most metadata historically attached to
// elements was per-Turn data being unnecessarily copied per-element. As
// genuine per-element use cases appear (e.g. per-element guardrail
// markers, tool-call IDs, error tags), they go here.
type ElementMetadata struct {
	// FromHistory marks elements emitted by load stages that represent
	// historical messages (vs the current input element of this Turn).
	// Read by stages that need to skip per-Turn work for historical
	// elements (e.g. TemplateStage no longer renders for history rows).
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

	// InterruptedTurnComplete marks the element that closes an
	// interrupted turn (downstream cleanup signal).
	InterruptedTurnComplete bool

	// ToolResultMessages carries tool-execution result messages produced
	// by the streaming tool integration; the duplex provider stage
	// consumes them as the next-round input.
	ToolResultMessages []types.Message

	// PendingTools carries tool calls that returned ToolStatusPending and
	// must be surfaced to the caller for out-of-band completion.
	PendingTools []tools.PendingToolExecution
}

// Transcription is the typed payload for duplex-provider input
// transcription attached to an element via ElementMetadata.
type Transcription struct {
	// Text is the transcribed user audio.
	Text string
}
