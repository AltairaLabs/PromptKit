package events

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func strPtr(s string) *string { return &s }

func messageWithEverything() *types.Message {
	return &types.Message{
		Role:    "assistant",
		Content: "here you go",
		Parts: []types.ContentPart{{
			Type: "image",
			Media: &types.MediaContent{
				Data:     strPtr("AAAABBBBCCCC"),
				MIMEType: "image/png",
			},
		}},
		ToolCalls: []types.MessageToolCall{{
			ID:   "call_1",
			Name: "lookup",
			Args: json.RawMessage(`{"q":"x"}`),
		}},
		ToolResult: &types.MessageToolResult{ID: "call_1", Name: "lookup"},
		Reasoning:  &types.ReasoningTrace{Text: "thought about it"},
	}
}

// TestNewMessageCreatedData_RoutesAgreeExceptParts is the assertion that stops
// the two routes drifting apart, which is how message.created came to have five
// payload shapes with the recording route never setting Index.
func TestNewMessageCreatedData_RoutesAgreeExceptParts(t *testing.T) {
	msg := messageWithEverything()

	recording := NewMessageCreatedData(msg, 7, false)
	live := NewMessageCreatedData(msg, 7, true)

	require.NotNil(t, recording)
	require.NotNil(t, live)

	assert.Equal(t, recording.Role, live.Role)
	assert.Equal(t, recording.Content, live.Content)
	assert.Equal(t, recording.Index, live.Index, "Index must be set on BOTH routes")
	assert.Equal(t, recording.ToolCalls, live.ToolCalls)
	assert.Equal(t, recording.ToolResult, live.ToolResult)
	assert.Equal(t, recording.Reasoning, live.Reasoning)
}

// TestNewMessageCreatedData_PartsDifferDeliberately pins the ONE intended
// difference, in both directions.
func TestNewMessageCreatedData_PartsDifferDeliberately(t *testing.T) {
	msg := messageWithEverything()

	recording := NewMessageCreatedData(msg, 0, false)
	require.Len(t, recording.Parts, 1)
	require.NotNil(t, recording.Parts[0].Media)
	require.NotNil(t, recording.Parts[0].Media.Data,
		"recording route keeps full binary for replay")
	assert.Equal(t, "AAAABBBBCCCC", *recording.Parts[0].Media.Data)

	live := NewMessageCreatedData(msg, 0, true)
	require.Len(t, live.Parts, 1)
	require.NotNil(t, live.Parts[0].Media)
	assert.Nil(t, live.Parts[0].Media.Data,
		"bus route strips binary so blobs stay out of observability")
	assert.Equal(t, "image/png", live.Parts[0].Media.MIMEType,
		"metadata is retained")
}

// TestNewMessageCreatedData_StrippingDoesNotMutateTheSource guards the hazard
// events.Redacting already documents: a bus fans one value out to several
// consumers, so rewriting in place would strip content from the recording that
// is supposed to keep it.
func TestNewMessageCreatedData_StrippingDoesNotMutateTheSource(t *testing.T) {
	msg := messageWithEverything()

	_ = NewMessageCreatedData(msg, 0, true)

	require.NotNil(t, msg.Parts[0].Media.Data,
		"stripping for the bus must not clear the caller's message")
	assert.Equal(t, "AAAABBBBCCCC", *msg.Parts[0].Media.Data)
}

func TestNewMessageCreatedData_IndexIsCarried(t *testing.T) {
	d := NewMessageCreatedData(&types.Message{Role: "user", Content: "hi"}, 42, true)
	require.NotNil(t, d)
	assert.Equal(t, 42, d.Index)
}

func TestNewMessageCreatedData_NilMessage(t *testing.T) {
	assert.Nil(t, NewMessageCreatedData(nil, 0, true))
}

// TestNewMessageCreatedData_ToolCallArgsAreStringified pins the conversion the
// event type requires: Message carries json.RawMessage, the event carries a
// string.
func TestNewMessageCreatedData_ToolCallArgsAreStringified(t *testing.T) {
	d := NewMessageCreatedData(messageWithEverything(), 0, true)
	require.Len(t, d.ToolCalls, 1)
	assert.Equal(t, "call_1", d.ToolCalls[0].ID)
	assert.Equal(t, "lookup", d.ToolCalls[0].Name)
	assert.JSONEq(t, `{"q":"x"}`, d.ToolCalls[0].Args)
}
