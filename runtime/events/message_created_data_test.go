package events

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

// TestNewMessageCreatedData_NilMessage pins both halves of the nil contract:
// a nil message produces no payload, AND that nil result is safe to read.
//
// Both matter. RecordingStage and MessageBroadcastStage each build from
// elem.Message, and a control element carries none — so nil reaches here in
// normal operation. Returning nil is only useful if consuming it is safe, which
// is why GetContent tolerates a nil receiver rather than leaving every consumer
// to nil-check before reading.
func TestNewMessageCreatedData_NilMessage(t *testing.T) {
	d := NewMessageCreatedData(nil, 7, true)

	require.Nil(t, d, "a nil message must not produce a payload")
	assert.Equal(t, "", d.GetContent(),
		"the nil result must be readable by a consumer, not a panic")
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

// TestNewMessageCreatedData_StripsToolResultBinary covers the other place
// binary hides. Only msg.Parts was stripped, so a tool returning an image or
// audio blob published the full base64 payload on the bus — and the OTel
// listener marshals the whole ToolResult into a span attribute when content
// capture is on.
func TestNewMessageCreatedData_StripsToolResultBinary(t *testing.T) {
	msg := &types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call_1",
			Name: "screenshot",
			Parts: []types.ContentPart{{
				Type:  "image",
				Media: &types.MediaContent{Data: strPtr("BLOBBLOBBLOB"), MIMEType: "image/png"},
			}},
		},
	}

	live := NewMessageCreatedData(msg, 0, true)
	require.NotNil(t, live.ToolResult)
	require.Len(t, live.ToolResult.Parts, 1)
	require.NotNil(t, live.ToolResult.Parts[0].Media)
	assert.Nil(t, live.ToolResult.Parts[0].Media.Data,
		"tool-result binary must not reach the bus")
	assert.Equal(t, "image/png", live.ToolResult.Parts[0].Media.MIMEType)

	recording := NewMessageCreatedData(msg, 0, false)
	require.NotNil(t, recording.ToolResult.Parts[0].Media.Data,
		"the recording route keeps it")

	require.NotNil(t, msg.ToolResult.Parts[0].Media.Data,
		"stripping must not clear the caller's message")
}

// TestMessageCreatedData_GetContent_ReadsWhicheverFieldCarriesTheText is the
// regression for something only a live turn exposed.
//
// A real Claude turn published a user message with Content "" and the text in
// Parts[0].Text, and an assistant message with Content set and no Parts. A
// consumer reading .Content — which the docs for this route originally said to
// do — renders a blank user turn for every conversation.
func TestMessageCreatedData_GetContent_ReadsWhicheverFieldCarriesTheText(t *testing.T) {
	cases := []struct {
		name string
		data *MessageCreatedData
		want string
	}{
		{
			name: "user message carries text in Parts, as the SDK builds it",
			data: &MessageCreatedData{Role: "user", Parts: []types.ContentPart{textPart("hello")}},
			want: "hello",
		},
		{
			name: "assistant reply carries text in Content",
			data: &MessageCreatedData{Role: "assistant", Content: "OK"},
			want: "OK",
		},
		{
			name: "tool message reads its result",
			data: &MessageCreatedData{
				Role:       "tool",
				ToolResult: &MessageToolResult{Parts: []types.ContentPart{textPart("42")}},
			},
			want: "42",
		},
		{
			name: "Parts win over Content when both are set",
			data: &MessageCreatedData{Role: "user", Content: "legacy", Parts: []types.ContentPart{textPart("modern")}},
			want: "modern",
		},
		{
			name: "media-only parts fall back to Content rather than returning empty",
			data: &MessageCreatedData{
				Role:    "user",
				Content: "caption",
				Parts:   []types.ContentPart{{Type: "image", Media: &types.MediaContent{MIMEType: "image/png"}}},
			},
			want: "caption",
		},
		{name: "nil is empty, not a panic", data: nil, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.data.GetContent())
		})
	}
}
