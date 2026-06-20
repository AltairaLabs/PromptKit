package tools

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// These MarshalJSON guards ensure the non-omitempty json.RawMessage fields on
// tool records never crash json.Marshal when empty (the "unexpected end of JSON
// input" failure). They normalize empty -> {} at the type boundary so a tool
// with no schema, or a tool call/result with no payload, can't lose a saved run
// or break a request. See types.NormalizeRawMessage.

// MarshalJSON normalizes the descriptor's schemas (empty -> {}).
func (d ToolDescriptor) MarshalJSON() ([]byte, error) { //nolint:gocritic // value receiver required for json.Marshaler
	type alias ToolDescriptor
	a := alias(d)
	a.InputSchema = types.NormalizeRawMessage(a.InputSchema)
	a.OutputSchema = types.NormalizeRawMessage(a.OutputSchema)
	return json.Marshal(a)
}

// MarshalJSON normalizes the tool call's args (empty -> {}).
func (c ToolCall) MarshalJSON() ([]byte, error) {
	type alias ToolCall
	a := alias(c)
	a.Args = types.NormalizeRawMessage(a.Args)
	return json.Marshal(a)
}

// MarshalJSON normalizes the tool result payload (empty -> {}).
func (r ToolResult) MarshalJSON() ([]byte, error) { //nolint:gocritic // value receiver required for json.Marshaler
	type alias ToolResult
	a := alias(r)
	a.Result = types.NormalizeRawMessage(a.Result)
	return json.Marshal(a)
}

// MarshalJSON normalizes the pending tool call's args (empty -> {}).
func (p PendingToolInfo) MarshalJSON() ([]byte, error) { //nolint:gocritic // value receiver required for json.Marshaler
	type alias PendingToolInfo
	a := alias(p)
	a.Args = types.NormalizeRawMessage(a.Args)
	return json.Marshal(a)
}
