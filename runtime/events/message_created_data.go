package events

import "github.com/AltairaLabs/PromptKit/runtime/types"

// NewMessageCreatedData builds the payload for a message.created event.
//
// Every producer of a COMPLETE message goes through here — RecordingStage on
// the store route, MessageBroadcastStage on the bus route — so the two cannot
// drift. They had drifted: the recording route kept full binary and never set
// Index, while the emitter stripped binary and did set it, so a consumer
// attached to both saw different data under one type name.
//
// stripBinary is the ONE deliberate difference, and it is the documented
// purpose of the two routes. The recording route keeps binary because its job
// is lossless replay; the bus route strips to metadata so large payloads do not
// flow through observability.
//
// The returned value never aliases the caller's Parts when stripping, so a
// consumer holding the source message is unaffected — the same hazard
// events.Redacting guards, for the same reason: one message fans out to
// several consumers with different entitlements.
//
// This does NOT cover the streaming-text, image and video producers in
// RecordingStage, which reuse message.created for fragments rather than
// messages. See MessageCreatedData.
func NewMessageCreatedData(msg *types.Message, index int, stripBinary bool) *MessageCreatedData {
	if msg == nil {
		return nil
	}

	parts := msg.Parts
	if stripBinary {
		// MetadataOnlyParts copies; it does not rewrite in place.
		parts = types.MetadataOnlyParts(parts)
	}

	data := &MessageCreatedData{
		Role:      msg.Role,
		Content:   msg.Content,
		Index:     index,
		Parts:     parts,
		Reasoning: msg.Reasoning,
	}

	if len(msg.ToolCalls) > 0 {
		data.ToolCalls = make([]MessageToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			data.ToolCalls[i] = MessageToolCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: string(tc.Args),
			}
		}
	}

	if msg.ToolResult != nil {
		// Tool results carry content parts too, and a tool that returns an
		// image or audio blob puts binary here. Stripping msg.Parts alone left
		// that payload on the bus — and the OTel listener marshals the whole
		// ToolResult into a span attribute when content capture is on.
		resultParts := msg.ToolResult.Parts
		if stripBinary {
			resultParts = types.MetadataOnlyParts(resultParts)
		}
		data.ToolResult = &MessageToolResult{
			ID:    msg.ToolResult.ID,
			Name:  msg.ToolResult.Name,
			Parts: resultParts,
		}
	}

	return data
}
