package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Message represents a single message in a conversation.
// This is the canonical message type used throughout the system.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant", "tool"
	Content string `json:"content"` // Message content (legacy text-only, maintained for backward compatibility)

	// Multimodal content parts (text, images, audio, video)
	// If Parts is non-empty, it takes precedence over Content.
	// For backward compatibility, if Parts is empty, Content will be used.
	Parts []ContentPart `json:"parts,omitempty"`

	// Tool invocations (for assistant messages that call tools)
	ToolCalls []MessageToolCall `json:"tool_calls,omitempty"`

	// Tool result (for tool role messages)
	// When Role="tool", this contains the tool execution result
	ToolResult *MessageToolResult `json:"tool_result,omitempty"`

	// Source indicates where this message originated (runtime-only, not persisted in JSON)
	// Values: "statestore" (loaded from StateStore), "pipeline" (created during execution), "" (user input),
	// "recorded_input" (user input the input RecordingStage has already recorded)
	Source string `json:"-"`

	// Metadata for observability and tracking
	Timestamp time.Time `json:"timestamp,omitempty"`  // When the message was created
	LatencyMs int64     `json:"latency_ms,omitempty"` // Time taken to generate (for assistant messages)
	CostInfo  *CostInfo `json:"cost_info,omitempty"`  // Token usage and cost tracking
	// Provider-reported reason the assistant stopped, normalized onto the
	// canonical vocabulary (see FinishReason* constants). Empty if unreported.
	FinishReason string                 `json:"finish_reason,omitempty"`
	Meta         map[string]interface{} `json:"meta,omitempty"` // Custom metadata

	// Reasoning holds the turn's model reasoning, off Parts so it is not content
	// and is excluded from future-turn context by default. See ReasoningTrace.
	Reasoning *ReasoningTrace `json:"reasoning,omitempty"`

	// Validation results (for assistant messages)
	Validations []ValidationResult `json:"validations,omitempty"`
}

// MetaToolsOffered is the Message.Meta key carrying the tool names the turn
// handed the provider for the round that produced this message.
//
// It records what the model COULD call, which no other record captures:
// ToolCalls say what it chose to do, and a tool the model was offered but never
// used leaves no trace anywhere else. That is the difference between a skill's
// allowed-tools grant that works and one that silently does nothing.
//
// The value is []string when freshly stamped and []any after a JSON round-trip
// through a state store; read it with evals.ExtractToolsOffered, which handles
// both.
const MetaToolsOffered = "_tools_offered"

// Canonical finish reasons. Each provider normalizes its raw wire value onto
// one of these before populating Message.FinishReason, so consumers can
// reliably detect e.g. an output-cap truncation regardless of provider. An
// unknown provider value passes through verbatim rather than mapping here.
const (
	FinishReasonStop            = "stop"              // natural completion
	FinishReasonMaxOutputTokens = "max_output_tokens" // output budget (incl. reasoning) exhausted
	FinishReasonToolUse         = "tool_use"          // stopped to call tools
	FinishReasonSafety          = "safety"            // blocked by safety / content filter
	FinishReasonRefusal         = "refusal"           // model declined to answer
	// FinishReasonInterrupted marks a partial reply kept from a stream that
	// failed or was canceled before the model finished. It is set by the
	// runtime, never by a provider, and the message is kept in the transcript
	// but never sent back to the model. Meta[MetaInterruptedCause] says why.
	FinishReasonInterrupted = "interrupted"
)

// MetaInterruptedCause is the Message.Meta key holding why an interrupted
// reply stopped: the error text of the stream failure or cancellation.
const MetaInterruptedCause = "_interrupted_cause"

// MetaBlockedInput is the Message.Meta key, set to true, that marks the canned
// assistant reply substituted for a user turn an input guardrail or a
// provider BeforeCall hook blocked. The reply and the blocked user message
// before it are kept in the transcript but never sent to the model.
const MetaBlockedInput = "_blocked_input"

// Message.Meta keys a realtime voice session sets on a reply the caller did
// not hear in full, when it tracks playout (the SDK's OpenVoice and
// WithPlayoutTracking). The reply keeps its own FinishReason: it completed,
// and only its playback was cut, so it stays in model context with any tool
// calls it made.
const (
	// MetaHeardMs is how much of the reply's audio the caller heard, in
	// milliseconds (int64).
	MetaHeardMs = "heard_ms"
	// MetaAudioMs is the reply's audio duration, in milliseconds (int64).
	MetaAudioMs = "audio_ms"
	// MetaPlayedFraction is MetaHeardMs over MetaAudioMs, from 0 to 1
	// (float64, three decimals).
	MetaPlayedFraction = "played_fraction"
	// MetaPlayoutCut says why the reply's playback was cut: PlayoutCutBargeIn,
	// PlayoutCutGuardrail or PlayoutCutSessionClosed.
	MetaPlayoutCut = "playout_cut"
)

// MetaCallerTurn is the Message.Meta key under which a duplex (realtime or
// voice) pipeline stores, on each reply, the 0-based index of the caller turn
// it answers (int; a number, once a store has encoded it): the turn index its
// guardrails and turn evals reported. A session that resumes the conversation
// goes on from it, so a spoken turn no transcript was stored for still
// counts.
const MetaCallerTurn = "caller_turn"

// Values of MetaPlayoutCut.
const (
	// PlayoutCutBargeIn: the caller barged in while the reply played.
	PlayoutCutBargeIn = "barge-in"
	// PlayoutCutSessionClosed: the session ended (closed, drained or canceled)
	// while the reply played.
	PlayoutCutSessionClosed = "session-closed"
	// PlayoutCutGuardrail: a guardrail blocked the reply and cut it.
	PlayoutCutGuardrail = "guardrail"
)

// MetaMessageMetadata is the Message.Meta key holding the metadata an
// application attached to this message (the SDK's WithMessageMetadata): what
// the user selected, the view they were on, anything that is not chat text.
//
// It is one key holding the application's map, not keys merged into Meta, so
// application keys can never collide with the runtime's own. Read it with
// Message.Metadata. It is stored with the message and carried on its
// message.created event, and never sent to a provider.
const MetaMessageMetadata = "message_metadata"

// MetaExcludedFromModel is the Message.Meta key, set to true, that keeps a
// message out of model context on its own: the transcript keeps it, but no
// provider request, summary, extracted memory or retrieval index is built
// from it. Unlike MetaBlockedInput on a canned reply it never takes the
// message before it out too. A host that rebuilds a history from a client's
// copy marks with it each message the stored history kept out of model
// context (the SDK's ReplaceHistory does), so the client's ordering cannot
// let it back in.
const MetaExcludedFromModel = "_excluded_from_model"

// MetaInjectedContext is the Message.Meta key holding text a host put in front
// of a user's message as its first text part: context the application
// supplied with the message (the AG-UI adapter's WithInputContext), not the
// user's words. A host comparing the message with a client's copy of it
// compares the text after it.
const MetaInjectedContext = "_injected_context"

// MetaMessageID is the Message.Meta key holding the message's stable id: a
// string the runtime assigns when it first stores the message (see
// EnsureMessageID), kept with the message in every state store, so the
// message reads back under the same id however often the history is loaded.
// A message stored before ids were assigned has none; ID returns "" for it.
const MetaMessageID = "_message_id"

// ID returns the message's stable id (MetaMessageID), or "" when it has none.
func (m *Message) ID() string {
	if m == nil {
		return ""
	}
	id, _ := m.Meta[MetaMessageID].(string)
	return id
}

// NewMessageID returns a new, random stable message id (see MetaMessageID).
func NewMessageID() string {
	return "msg_" + uuid.NewString()
}

// EnsureMessageID returns msg carrying a stable id: msg itself when it has one
// (ID), otherwise a copy with a new, random id under MetaMessageID. The copy
// gets its own Meta map, so the id never reaches another holder of msg's Meta.
func EnsureMessageID(msg Message) Message {
	if msg.ID() != "" {
		return msg
	}
	meta := make(map[string]interface{}, len(msg.Meta)+1)
	for k, v := range msg.Meta {
		meta[k] = v
	}
	meta[MetaMessageID] = NewMessageID()
	msg.Meta = meta
	return msg
}

// Metadata returns a copy of the metadata an application attached to the
// message (see MetaMessageMetadata), or nil when there is none. The copy is
// deep, so changing it never changes the message, or any other holder of the
// message's Meta.
func (m *Message) Metadata() map[string]any {
	if m == nil {
		return nil
	}
	md, _ := m.Meta[MetaMessageMetadata].(map[string]any)
	if md == nil {
		return nil
	}
	cp, _ := cloneJSONValue(md).(map[string]any)
	return cp
}

// cloneJSONValue deep-copies the maps and slices of a decoded JSON value.
// Anything else is returned as is: scalars are immutable, and other types do
// not occur in metadata, which is stored in its decoded JSON form.
func cloneJSONValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, inner := range val {
			out[k] = cloneJSONValue(inner)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, inner := range val {
			out[i] = cloneJSONValue(inner)
		}
		return out
	default:
		return v
	}
}

// MessageToolCall represents a request to call a tool within a Message.
// The Args field contains the JSON-encoded arguments for the tool.
type MessageToolCall struct {
	ID   string          `json:"id"`   // Unique identifier for this tool call
	Name string          `json:"name"` // Name of the tool to invoke
	Args json.RawMessage `json:"args"` // JSON-encoded tool arguments

	// ProviderMetadata carries opaque provider-specific fields that must be
	// round-tripped back to the provider on subsequent turns. Keys are
	// namespaced by provider (e.g. "gemini.thought_signature"). This is
	// required for providers like Gemini 3 that emit signed tool-call
	// parts which must be replayed verbatim to pass server-side validation.
	ProviderMetadata map[string]string `json:"provider_metadata,omitempty"`
}

// MessageToolResult represents the result of a tool execution in a Message.
// When embedded in Message, the Message.Role should be "tool".
type MessageToolResult struct {
	ID        string        `json:"id"`                   // References the MessageToolCall.ID that triggered this result
	Name      string        `json:"name"`                 // Tool name that was executed
	Error     string        `json:"error,omitempty"`      // Error message if tool execution failed
	ErrorType ToolErrorType `json:"error_type,omitempty"` // Classification of failure reason

	// Parts contains the multimodal result content (text, images, etc.).
	Parts     []ContentPart `json:"parts,omitempty" yaml:"parts,omitempty"`
	LatencyMs int64         `json:"latency_ms"` // Tool execution latency in milliseconds
}

// GetTextContent returns concatenated text from all text parts.
// This is the recommended way to access tool result content as text.
func (r *MessageToolResult) GetTextContent() string {
	var sb strings.Builder
	for _, part := range r.Parts {
		if part.Type == ContentTypeText && part.Text != nil {
			sb.WriteString(*part.Text)
		}
	}
	return sb.String()
}

// HasMedia returns true if any non-text parts exist in the tool result.
func (r *MessageToolResult) HasMedia() bool {
	for _, part := range r.Parts {
		if part.Type != ContentTypeText {
			return true
		}
	}
	return false
}

// NewTextToolResult creates a MessageToolResult with text-only content.
// This is the most common case and should be used everywhere that would
// otherwise set a text string Content.
func NewTextToolResult(id, name, text string) MessageToolResult {
	return MessageToolResult{
		ID:   id,
		Name: name,
		Parts: []ContentPart{
			NewTextPart(text),
		},
	}
}

// ToolDef represents a tool definition that can be provided to an LLM.
// The InputSchema and OutputSchema use JSON Schema format for validation.
type ToolDef struct {
	Name         string          `json:"name"`                    // Unique tool name
	Description  string          `json:"description"`             // Human-readable description of what the tool does
	InputSchema  json.RawMessage `json:"input_schema"`            // JSON Schema for input validation
	OutputSchema json.RawMessage `json:"output_schema,omitempty"` // Optional JSON Schema for output validation
}

// emptyJSONObject is substituted for an empty/nil json.RawMessage so json.Marshal
// never fails with "unexpected end of JSON input".
var emptyJSONObject = json.RawMessage("{}")

// NormalizeRawMessage returns a valid JSON value for raw, substituting "{}" when
// raw is nil, empty, OR not well-formed JSON. Two crash sources in the agent loop
// motivate this: LLMs routinely emit tool calls with no arguments (empty), and a
// streamed tool call cut off at max_tokens leaves *truncated* args (e.g.
// `{"path":"foo`) — non-empty but invalid. Both fail json.RawMessage's encoder
// with "unexpected end of JSON input". Provider request builders and the message
// types call this at the boundary so a missing or broken blob degrades to {}
// instead of crashing a request or losing a whole run's saved output.
func NormalizeRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return emptyJSONObject
	}
	return raw
}

// MarshalJSON ensures Args is always valid JSON (empty -> {}). A tool call with
// no arguments must never crash request building or result persistence.
func (mtc MessageToolCall) MarshalJSON() ([]byte, error) {
	type alias MessageToolCall
	a := alias(mtc)
	a.Args = NormalizeRawMessage(a.Args)
	return json.Marshal(a)
}

// MarshalJSON ensures InputSchema is always valid JSON (empty -> {}). A tool with
// no input schema must never crash a provider request.
func (td ToolDef) MarshalJSON() ([]byte, error) { // value receiver required for json.Marshaler
	type alias ToolDef
	a := alias(td)
	a.InputSchema = NormalizeRawMessage(a.InputSchema)
	return json.Marshal(a)
}

// CostInfo tracks token usage and associated costs for LLM operations.
// All cost values are in USD. Used for both individual messages and aggregated tracking.
type CostInfo struct {
	InputTokens   int     `json:"input_tokens"`              // Uncached input tokens across all modalities
	OutputTokens  int     `json:"output_tokens"`             // Visible output tokens across all modalities
	CachedTokens  int     `json:"cached_tokens,omitempty"`   // Cache-read tokens
	InputCostUSD  float64 `json:"input_cost_usd"`            // Cost of input tokens in USD
	OutputCostUSD float64 `json:"output_cost_usd"`           // Cost of output tokens in USD
	CachedCostUSD float64 `json:"cached_cost_usd,omitempty"` // Cost of cache-read tokens in USD
	TotalCost     float64 `json:"total_cost_usd"`            // Total cost in USD

	// Per-unit fields populated when a provider prices its usage.
	Quantities     map[string]float64 `json:"quantities,omitempty"`      // Raw billable quantities by unit name
	DimensionMatch map[string]string  `json:"dimension_match,omitempty"` // Dimension key-value pairs (e.g. size, quality)
	ProviderName   string             `json:"provider_name,omitempty"`   // Provider that produced this cost entry
	Model          string             `json:"model,omitempty"`           // Model that served the call, when known
	Capability     string             `json:"capability,omitempty"`      // ProviderType as string for JSON portability
	Latency        time.Duration      `json:"latency_ns,omitempty"`      // Time taken by the provider call

	// Breakdown is a per-line-item view of the cost. A priced provider call
	// fills it with one item per billed unit. On the aggregated rollup it merges
	// the items of every contributing message by (provider, capability, unit,
	// dimensions), so a report can show per-unit detail.
	Breakdown []CostLineItem `json:"breakdown,omitempty"`
}

// CostLineItem is one row of a cost breakdown, suitable for surfacing in
// reports. Carries provider + capability tags so consumers can group across
// the conversation without re-deriving them.
type CostLineItem struct {
	Provider   string            `json:"provider,omitempty"`
	Capability string            `json:"capability,omitempty"`
	Unit       string            `json:"unit"`
	Quantity   float64           `json:"quantity"`
	USD        float64           `json:"usd"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// ToolStats tracks tool usage statistics across a conversation or run.
// Useful for monitoring which tools are being used and how frequently.
type ToolStats struct {
	TotalCalls int            `json:"total_calls"` // Total number of tool calls
	ByTool     map[string]int `json:"by_tool"`     // Count of calls per tool name
}

// ValidationError represents a validation failure in tool usage or message content.
// It provides structured error information when validation fails.
type ValidationError struct {
	Type   string `json:"type"`   // Error type: "args_invalid" | "result_invalid" | "policy_violation"
	Tool   string `json:"tool"`   // Name of the tool that failed validation
	Detail string `json:"detail"` // Human-readable error details
}

// ValidationResult represents the outcome of a validator check on a message.
// These are attached to assistant messages to show which validations passed or failed.
type ValidationResult struct {
	ValidatorType string                 `json:"validator_type"`      // Type of validator
	Passed        bool                   `json:"passed"`              // Whether the validation passed
	Details       map[string]interface{} `json:"details,omitempty"`   // Validator-specific details
	Timestamp     time.Time              `json:"timestamp,omitempty"` // When validation was performed
}

// roleTool is the Role of a message carrying a tool result.
const roleTool = "tool"

// GetContent returns the content of the message.
// This is the recommended way to access message content as it handles all cases:
// 1. For tool messages (Role="tool") with a ToolResult: returns ToolResult.GetTextContent()
// 2. For multimodal messages: returns concatenated text parts
// 3. For legacy messages: returns the Content field
func (m *Message) GetContent() string {
	// Tool messages: ToolResult.Parts is the authoritative source
	// This handles the case where Content field may be empty but ToolResult.Parts is set
	if m.Role == roleTool && m.ToolResult != nil {
		return m.ToolResult.GetTextContent()
	}

	// Multimodal messages: extract text from parts
	if len(m.Parts) > 0 {
		var text string
		for _, part := range m.Parts {
			if part.Type == ContentTypeText && part.Text != nil {
				text += *part.Text
			}
		}
		return text
	}

	return m.Content
}

// StripReasoning returns copies of msgs with Reasoning cleared, for persistence
// or export paths that must not retain model reasoning (the default behavior;
// see Message.Reasoning). The input slice and its messages are not mutated.
func StripReasoning(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].Reasoning = nil
	}
	return out
}

// IsInterrupted reports whether m is a partial reply kept from a stream that
// failed before the model finished (see FinishReasonInterrupted).
func (m *Message) IsInterrupted() bool {
	return m.FinishReason == FinishReasonInterrupted
}

// ExcludeInterrupted returns msgs without interrupted replies, for any path
// that sends history back to a model. A half-finished reply would mislead the
// model, and one cut off mid tool call leaves a call with no result, which
// providers reject. The transcript keeps them; only model context drops them.
// msgs is returned as-is when it holds none.
//
// What a caller heard of a spoken reply they cut off (IsHeardPrefix) is kept:
// it was said, so the model goes on from it, as a realtime model goes on from
// the reply its provider truncated to what was heard.
func ExcludeInterrupted(msgs []Message) []Message {
	n := 0
	for i := range msgs {
		if msgs[i].LeavesModelContext() {
			n++
		}
	}
	if n == 0 {
		return msgs
	}
	out := make([]Message, 0, len(msgs)-n)
	for i := range msgs {
		if !msgs[i].LeavesModelContext() {
			out = append(out, msgs[i])
		}
	}
	return out
}

// IsHeardPrefix reports whether m is what a caller heard of a spoken reply
// they cut off: an interrupted reply whose playback was cut (Meta
// MetaPlayoutCut), its text what had played, with no tool calls (a call cut
// off may have no result, which providers reject).
func (m *Message) IsHeardPrefix() bool {
	if !m.IsInterrupted() || len(m.ToolCalls) > 0 {
		return false
	}
	_, cut := m.Meta[MetaPlayoutCut]
	return cut
}

// LeavesModelContext reports whether m is left out of history sent back to a
// model (ExcludeInterrupted): an interrupted reply that is not what a caller
// heard of it.
func (m *Message) LeavesModelContext() bool {
	return m.IsInterrupted() && !m.IsHeardPrefix()
}

// IsMultimodal returns true if the message contains multimodal content (Parts)
func (m *Message) IsMultimodal() bool {
	return len(m.Parts) > 0
}

// HasMediaContent returns true if the message contains any media (image, audio, video, document)
func (m *Message) HasMediaContent() bool {
	for _, part := range m.Parts {
		if part.Type == ContentTypeImage || part.Type == ContentTypeAudio ||
			part.Type == ContentTypeVideo || part.Type == ContentTypeDocument {
			return true
		}
	}
	return false
}

// SetTextContent sets the message content to simple text.
// This clears any existing Parts and sets the legacy Content field.
func (m *Message) SetTextContent(text string) {
	m.Content = text
	m.Parts = nil
}

// SetMultimodalContent sets the message content to multimodal parts.
// This clears the legacy Content field.
func (m *Message) SetMultimodalContent(parts []ContentPart) {
	m.Parts = parts
	m.Content = ""
}

// AddPart adds a content part to the message.
// If this is the first part added, it clears the legacy Content field.
func (m *Message) AddPart(part ContentPart) {
	if len(m.Parts) == 0 {
		m.Content = ""
	}
	m.Parts = append(m.Parts, part)
}

// AddTextPart adds a text content part to the message
func (m *Message) AddTextPart(text string) {
	m.AddPart(NewTextPart(text))
}

// AddImagePart adds an image content part from a file path
func (m *Message) AddImagePart(filePath string, detail *string) error {
	part, err := NewImagePart(filePath, detail)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// AddImagePartFromURL adds an image content part from a URL
func (m *Message) AddImagePartFromURL(url string, detail *string) {
	m.AddPart(NewImagePartFromURL(url, detail))
}

// AddAudioPart adds an audio content part from a file path
func (m *Message) AddAudioPart(filePath string) error {
	part, err := NewAudioPart(filePath)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// AddVideoPart adds a video content part from a file path
func (m *Message) AddVideoPart(filePath string) error {
	part, err := NewVideoPart(filePath)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// AddDocumentPart adds a document content part from a file path
func (m *Message) AddDocumentPart(filePath string) error {
	part, err := NewDocumentPart(filePath)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// =============================================================================
// Message Constructors
//
// These constructors ensure proper initialization and field synchronization.
// Always prefer these over direct struct initialization.
// =============================================================================

// NewTextMessage creates a simple text message.
// Use this for user, assistant, or system messages with text-only content.
func NewTextMessage(role, content string) Message {
	return Message{
		Role:    role,
		Content: content,
	}
}

// NewUserMessage creates a user message with text content.
func NewUserMessage(content string) Message {
	return NewTextMessage("user", content)
}

// NewAssistantMessage creates an assistant message with text content.
func NewAssistantMessage(content string) Message {
	return NewTextMessage("assistant", content)
}

// NewSystemMessage creates a system message with text content.
func NewSystemMessage(content string) Message {
	return NewTextMessage("system", content)
}

// NewToolResultMessage creates a properly normalized tool result message.
// This ensures Content and ToolResult.Parts are always synchronized,
// which is required for provider compatibility and consistent behavior.
//
// IMPORTANT: Always use this constructor instead of directly creating
// Message{Role: "tool", ToolResult: ...} to avoid Content/ToolResult.Parts
// synchronization issues.
func NewToolResultMessage(result MessageToolResult) Message {
	return Message{
		Role:       roleTool,
		Content:    result.GetTextContent(), // Sync Content for backward compatibility
		ToolResult: &result,
	}
}

// NewMultimodalMessage creates a message with multimodal content parts.
// When using Parts, the Content field is intentionally left empty as
// GetContent() will extract text from Parts.
func NewMultimodalMessage(role string, parts []ContentPart) Message {
	return Message{
		Role:  role,
		Parts: parts,
	}
}

// MediaSummary provides a high-level overview of media content in a message.
// This is included in JSON output to make multimodal messages more observable.
type MediaSummary struct {
	TotalParts    int                `json:"total_parts"`           // Total number of content parts
	TextParts     int                `json:"text_parts"`            // Number of text parts
	ImageParts    int                `json:"image_parts"`           // Number of image parts
	AudioParts    int                `json:"audio_parts"`           // Number of audio parts
	VideoParts    int                `json:"video_parts"`           // Number of video parts
	DocumentParts int                `json:"document_parts"`        // Number of document parts
	MediaItems    []MediaItemSummary `json:"media_items,omitempty"` // Details of each media item
}

// MediaItemSummary provides details about a single media item in a message.
type MediaItemSummary struct {
	Type      string `json:"type"`             // Content type: "image", "audio", "video", "document"
	Source    string `json:"source"`           // Source description (file path, URL, or "inline data")
	MIMEType  string `json:"mime_type"`        // MIME type
	SizeBytes int    `json:"size_bytes"`       // Size in bytes (0 if unknown)
	Detail    string `json:"detail,omitempty"` // Detail level for images
	Loaded    bool   `json:"loaded"`           // Whether media was successfully loaded
	Error     string `json:"error,omitempty"`  // Error message if loading failed
}

// MarshalJSON implements custom JSON marshaling for Message.
// This enhances the output by:
// 1. Populating the Content field with a human-readable summary when Parts exist
// 2. Adding a MediaSummary field for observability of multimodal content
// 3. Omitting Content field when ToolResult is present to avoid duplication
func (m Message) MarshalJSON() ([]byte, error) {
	// Create a type alias to avoid infinite recursion
	type MessageAlias Message

	// Convert to alias to get default marshaling behavior
	aux := struct {
		MessageAlias
		MediaSummary *MediaSummary `json:"media_summary,omitempty"`
		Content      string        `json:"content,omitempty"` // Override to control omission
	}{
		MessageAlias: MessageAlias(m),
		Content:      m.Content, // Default to actual content
	}

	// If Parts exist and Content is empty, populate content with summary
	if len(m.Parts) > 0 && m.Content == "" {
		aux.Content = m.getContentSummary()
	}

	// Add media summary if Parts exist
	if len(m.Parts) > 0 {
		aux.MediaSummary = m.getMediaSummary()
	}

	// If ToolResult is present, omit Content to avoid duplication
	if m.ToolResult != nil {
		aux.Content = "" // Empty string with omitempty will omit the field
	}

	return json.Marshal(aux)
}

// UnmarshalJSON implements custom JSON unmarshaling for Message.
// After unmarshaling, if ToolResult is present, copy its Content to Message.Content
// for provider compatibility (providers expect Content field to be populated).
func (m *Message) UnmarshalJSON(data []byte) error {
	type MessageAlias Message
	aux := (*MessageAlias)(m)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	// If tool result is present but Content is empty, copy text from ToolResult.Parts
	if m.ToolResult != nil && m.Content == "" {
		m.Content = m.ToolResult.GetTextContent()
	}
	return nil
}

// contentSummaryKinds lists, in order, the non-text part types that
// getContentSummary counts and the label it gives each.
var contentSummaryKinds = []struct {
	contentType string
	label       string
}{
	{ContentTypeImage, "image(s)"},
	{ContentTypeAudio, "audio file(s)"},
	{ContentTypeVideo, "video(s)"},
	{ContentTypeDocument, "document(s)"},
}

// getContentSummary returns a human-readable summary of the message content.
// For multimodal messages, this includes text parts and a summary of media.
func (m *Message) getContentSummary() string {
	if len(m.Parts) == 0 {
		return m.Content
	}

	var parts []string

	// Collect text parts
	for _, part := range m.Parts {
		if part.Type == ContentTypeText && part.Text != nil {
			parts = append(parts, *part.Text)
		}
	}

	// Add media summary
	mediaCounts := make(map[string]int)
	for _, part := range m.Parts {
		mediaCounts[part.Type]++
	}

	var mediaDesc []string
	for _, kind := range contentSummaryKinds {
		if count := mediaCounts[kind.contentType]; count > 0 {
			mediaDesc = append(mediaDesc, fmt.Sprintf("%d %s", count, kind.label))
		}
	}
	if len(mediaDesc) > 0 {
		parts = append(parts, "["+strings.Join(mediaDesc, ", ")+"]")
	}

	return strings.Join(parts, " ")
}

// getMediaSummary generates a MediaSummary with details about all media in the message.
func (m *Message) getMediaSummary() *MediaSummary {
	if len(m.Parts) == 0 {
		return nil
	}

	summary := &MediaSummary{
		TotalParts: len(m.Parts),
		MediaItems: make([]MediaItemSummary, 0),
	}

	for _, part := range m.Parts {
		switch part.Type {
		case ContentTypeText:
			summary.TextParts++
		case ContentTypeImage:
			summary.ImageParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		case ContentTypeAudio:
			summary.AudioParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		case ContentTypeVideo:
			summary.VideoParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		case ContentTypeDocument:
			summary.DocumentParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		}
	}

	return summary
}

// estimateBase64DecodedSize returns the approximate decoded-byte count for a
// base64-encoded string. Every 4 base64 chars encode 3 bytes of payload; we
// ignore padding for this rough estimate.
func estimateBase64DecodedSize(data string) int {
	const (
		base64Ratio     = 4
		base64Numerator = 3
	)
	return (len(data) * base64Numerator) / base64Ratio
}

// hasInlineData returns true when a MediaContent carries a non-empty base64 Data string.
func hasInlineData(media *MediaContent) bool {
	return media.Data != nil && *media.Data != ""
}

// populateMediaSource fills Source/Loaded/SizeBytes on item based on which
// MediaContent field identifies the media. FilePath/URL/StorageReference take
// priority over inline data for display; the SizeBytes estimate is only
// applied on the FilePath and inline-data branches to preserve the
// pre-refactor behavior.
func populateMediaSource(item *MediaItemSummary, media *MediaContent) {
	switch {
	case media.FilePath != nil:
		item.Source = *media.FilePath
		if hasInlineData(media) {
			item.Loaded = true
			item.SizeBytes = estimateBase64DecodedSize(*media.Data)
		}
	case media.URL != nil:
		item.Source = *media.URL
		if hasInlineData(media) {
			item.Loaded = true
		}
	case media.StorageReference != nil:
		// StorageReference means media was externalized to storage; we don't
		// mark Loaded here because the reference alone doesn't imply the data
		// has been fetched into memory.
		item.Source = *media.StorageReference
	case hasInlineData(media):
		item.Source = "inline data"
		item.Loaded = true
		item.SizeBytes = estimateBase64DecodedSize(*media.Data)
	default:
		item.Source = "unknown"
		item.Error = "no data source"
	}
}

// getMediaItemSummary extracts summary information from a media ContentPart.
func getMediaItemSummary(part ContentPart) MediaItemSummary {
	item := MediaItemSummary{
		Type:   part.Type,
		Loaded: false, // Will be true if Data is set
	}

	if part.Media == nil {
		item.Error = "no media content"
		return item
	}

	item.MIMEType = part.Media.MIMEType
	populateMediaSource(&item, part.Media)

	if part.Type == ContentTypeImage && part.Media.Detail != nil {
		item.Detail = *part.Media.Detail
	}

	// SizeKB metadata overrides any base64-derived estimate.
	if part.Media.SizeKB != nil {
		const bytesPerKB = 1024
		item.SizeBytes = int(*part.Media.SizeKB * bytesPerKB)
	}

	return item
}
