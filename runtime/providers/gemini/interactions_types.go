package gemini

import "encoding/json"

// Wire types for the v1beta/interactions API.
//
// This API is shaped differently from generateContent throughout: a flat list
// of typed steps rather than candidates/parts, tools declared flat rather than
// under functionDeclarations, and a response_format that constrains only the
// turn producing a final answer.
//
// It is used statelessly here — the full transcript is replayed as input on
// every call, with no previous_interaction_id — so the runtime keeps ownership
// of the transcript and the existing tool loop is unchanged. Verified live.

const interactionsPath = "/interactions"

// Step types seen on both the request (as replayed input) and the response.
const (
	stepTypeText           = "text"
	stepTypeThought        = "thought"
	stepTypeModelOutput    = "model_output"
	stepTypeFunctionCall   = "function_call"
	stepTypeFunctionResult = "function_result"
)

// kindInteractionsThought marks an OpaqueReasoning entry as an Interactions API
// thought signature, so it can be replayed as a thought step on later rounds.
const kindInteractionsThought = "interactions_thought_signature"

// interactionsRequest is one call to the Interactions API.
type interactionsRequest struct {
	Model string `json:"model"`
	// Input is the replayed transcript. A plain string is accepted for the
	// simplest case, but this provider always sends the typed list.
	Input          []any                  `json:"input"`
	Tools          []interactionsTool     `json:"tools,omitempty"`
	ResponseFormat *interactionsRespFmt   `json:"response_format,omitempty"`
	Stream         bool                   `json:"stream,omitempty"`
	Generation     map[string]any         `json:"generation_config,omitempty"`
	Extra          map[string]interface{} `json:"-"`
}

// interactionsTool is a flat function declaration, unlike generateContent's
// nested functionDeclarations.
type interactionsTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// interactionsRespFmt carries the caller's schema. Unlike generateContent's
// responseMimeType/responseSchema pair, this constrains only the answering turn.
type interactionsRespFmt struct {
	Type     string          `json:"type"`
	MIMEType string          `json:"mime_type,omitempty"`
	Schema   json.RawMessage `json:"schema,omitempty"`
}

// interactionsThoughtStep replays the model's reasoning.
//
// A thought step is an OPAQUE SIGNATURE, not text: the API returns
// {"type":"thought","signature":"…"} and rejects a `content` field. Replaying
// it is mandatory — a history containing a function_call without its preceding
// thought is refused with "Request contains an invalid argument", and an empty
// thought is refused too. The signature is therefore round-tripped through
// ReasoningTrace.Opaque, which exists for exactly this.
type interactionsThoughtStep struct {
	Type      string `json:"type"`
	Signature string `json:"signature"`
}

// interactionsTextStep is a plain user turn.
type interactionsTextStep struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// interactionsFunctionCall is the model asking for a tool. On replay it is sent
// back verbatim, which carries any signature through without special handling —
// the requirement that makes hand-built Gemini 3 histories fail.
// A function_call carries `id` only. `call_id` is rejected here ("Unknown
// parameter 'call_id'") and belongs on the function_result that answers it.
type interactionsFunctionCall struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

// interactionsFunctionResult answers one function_call.
//
// Result is a SINGLE content item, not a list. A list is treated as a
// multimodal response and rejected by Gemini 2.5 with "Multimodal function
// responses are not supported for this model"; the single form is accepted by
// both generations, so it is used unconditionally rather than gating on model.
type interactionsFunctionResult struct {
	Type   string              `json:"type"`
	Name   string              `json:"name"`
	CallID string              `json:"call_id,omitempty"`
	Result interactionsContent `json:"result"`
}

// interactionsContent is a content item inside a step.
type interactionsContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// interactionsResponse is a completed (non-streaming) interaction.
type interactionsResponse struct {
	ID     string             `json:"id"`
	Status string             `json:"status"`
	Model  string             `json:"model"`
	Steps  []interactionsStep `json:"steps"`
	Usage  *interactionsUsage `json:"usage,omitempty"`
	Error  *interactionsError `json:"error,omitempty"`
}

// interactionsStep is one step of the model's turn. Which fields are populated
// depends on Type.
type interactionsStep struct {
	Type      string                `json:"type"`
	Content   []interactionsContent `json:"content,omitempty"`
	Text      string                `json:"text,omitempty"`
	Name      string                `json:"name,omitempty"`
	ID        string                `json:"id,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Arguments json.RawMessage       `json:"arguments,omitempty"`
	Signature string                `json:"signature,omitempty"`
}

// text returns the step's text whether it arrives inline or as content items.
func (s *interactionsStep) text() string {
	if s.Text != "" {
		return s.Text
	}
	var out string
	for i := range s.Content {
		out += s.Content[i].Text
	}
	return out
}

type interactionsUsage struct {
	TotalTokens        int `json:"total_tokens"`
	TotalInputTokens   int `json:"total_input_tokens"`
	TotalOutputTokens  int `json:"total_output_tokens"`
	TotalCachedTokens  int `json:"total_cached_tokens"`
	TotalThoughtTokens int `json:"total_thought_tokens"`
}

type interactionsError struct {
	Message string `json:"message"`
	Status  string `json:"status"`
	Code    int    `json:"code"`
}
