package hooks

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Decision is the result of a hook evaluation.
type Decision struct {
	Allow    bool
	Reason   string
	Metadata map[string]any
	// Enforced indicates the hook already applied enforcement (e.g., truncated
	// or replaced content on the response). When true, the provider stage
	// records the validation result but continues the pipeline instead of
	// returning an error.
	Enforced bool

	// Hold is set by a chunk interceptor that allows a chunk: how many bytes
	// at the end of the chunk's Content it has not cleared yet, because they
	// could be the start of content it blocks once more of the reply arrives
	// (a banned word split across chunks). The provider stage keeps those
	// bytes from the caller until a later chunk clears them, completes what it
	// blocks, or the reply ends. Zero holds nothing back.
	Hold int
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

// Enforced creates an enforced decision: the hook applied enforcement
// (truncation, content replacement) and the pipeline continues.
func Enforced(reason string, metadata map[string]any) Decision {
	return Decision{Allow: false, Reason: reason, Metadata: metadata, Enforced: true}
}

// Guardrail directions: which side of a provider call a check gates, and the
// vocabulary a firing is tagged with.
//
// Both the guardrail adapter and the pipeline stage use these labels, so they
// live here rather than in hooks/guardrails.
const (
	// DirectionInput gates the user's input, in BeforeCall.
	DirectionInput = "input"
	// DirectionOutput gates the assistant's response, in AfterCall.
	DirectionOutput = "output"
	// DirectionBoth gates input and output.
	DirectionBoth = "both"
)

// ProviderRequest describes an LLM call about to be made.
type ProviderRequest struct {
	ProviderID   string
	Model        string
	Messages     []types.Message
	SystemPrompt string
	Round        int
	Metadata     map[string]any

	// TurnIndex is the 0-based turn this call belongs to: 0 for the
	// conversation's first turn, on the text path (Send, Stream) and the
	// realtime path alike, counted from the persisted transcript. A text
	// pipeline built without a state store (sdk.Open always has one) has no
	// transcript to count, and reports 0 on every turn. Round is not a substitute: it counts tool-loop
	// rounds within one turn and restarts in each composition sub-pipeline.
	TurnIndex int

	// Prompt is the pack's definition of the prompt the model is being
	// invoked for, a read-only copy taken when the conversation was built.
	// After a workflow handoff it is the destination state's prompt. Nil means
	// undeclared. PromptKit never reads it.
	Prompt *packspec.Prompt

	// Replacement is written by a BeforeCall hook that returns Enforced, to
	// supply the assistant text returned in place of the blocked provider
	// call. Mirrors how output guardrails mutate resp.Message in AfterCall.
	// Empty falls back to Decision.Metadata["replacement"], then to
	// prompt.DefaultBlockedMessage.
	Replacement string

	// Caller is who the conversation acts for, as the host set it with
	// sdk.WithCaller. The zero value means none was set. PromptKit hands it to
	// the host's provider hooks, guardrails included, and otherwise never
	// reads, persists, logs or emits it.
	Caller types.Caller
}

// ChunkRequest is a streamed response chunk handed to a
// ChunkRequestInterceptor, with who the call acts for.
type ChunkRequest struct {
	// Chunk is the chunk being judged. An interceptor that returns Enforced
	// rewrites Chunk.Content in place, as OnChunk does.
	Chunk *providers.StreamChunk

	// Caller is who the conversation acts for, as the host set it with
	// sdk.WithCaller. The zero value means none was set. PromptKit hands it to
	// the host's interceptor and otherwise never reads, persists, logs or
	// emits it.
	Caller types.Caller
}

// ProviderResponse describes a completed LLM call.
type ProviderResponse struct {
	ProviderID string
	Model      string
	Message    types.Message
	Round      int
	LatencyMs  int64
}

// InputRequest is the narrow view of an about-to-be-sent call handed to a
// func-based input guardrail. Use guardrails.InputFunc to build one.
type InputRequest struct {
	// UserInput is the text of the user message being gated.
	UserInput string
	// Messages is the full conversation history including that message.
	Messages []types.Message
	// Round is the provider round this call belongs to (1-based).
	Round int
	// Replacement supplies the canned assistant text when the check
	// returns Enforced. Empty falls back to the default blocked message.
	Replacement string
	// Caller is who the conversation acts for, as the host set it with
	// sdk.WithCaller. The zero value means none was set.
	Caller types.Caller
}

// OutputRequest is the narrow view of a completed call handed to a func-based
// output guardrail. Use guardrails.OutputFunc to build one.
type OutputRequest struct {
	// Content is the assistant text being gated.
	Content string
	// Message is the response message; mutate it in place to enforce.
	Message *types.Message
	// Round is the provider round this response belongs to (1-based).
	Round int
	// Caller is who the conversation acts for, as the host set it with
	// sdk.WithCaller. The zero value means none was set.
	Caller types.Caller
}

// MetadataKeyValidatorDeclaration is the Decision.Metadata key under which a
// pack-declared guardrail reports its *packspec.Validator, so the stage can put
// it on the validation event. The stage removes it before recording the
// decision on the message.
const MetadataKeyValidatorDeclaration = "validator_declaration"

// ToolRequest describes a tool call about to be executed.
type ToolRequest struct {
	Name   string
	Args   json.RawMessage
	CallID string

	// Declaration is the pack's definition of the tool, the same value its
	// executor receives on tools.ToolDescriptor.Declaration, so a host that
	// only allows or denies needs one hook covering every tool. Nil means the
	// pack does not declare the tool. PromptKit never reads it.
	Declaration *packspec.Tool

	// Caller is who the conversation acts for, as the host set it with
	// sdk.WithCaller. The zero value means none was set. PromptKit hands it to
	// the host's tool hooks, an exec hook included (as "Caller" in its stdin
	// JSON), and otherwise never reads, persists, logs or emits it.
	Caller types.Caller
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

	// Caller is who the conversation acts for, as the host set it with
	// sdk.WithCaller. The zero value means none was set. PromptKit hands it to
	// the host's session hooks, an exec hook included (as "Caller" in its stdin
	// JSON), and otherwise never reads, persists, logs or emits it.
	Caller types.Caller
}
