package sdk

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// WithMessageMetadata attaches key-value metadata to the user message of a
// single Send or Stream call: information the application has about the turn
// that is not chat text, such as which shapes the user selected on a canvas or
// the view they were on.
//
// The metadata is stored with the message, so it is in the conversation's
// message history (Messages, the state store) and on the message's
// message.created event (event bus and event store), where events.Redacting
// rewrites its strings. Evals read the conversation's messages, so they see it
// too. It is never sent to the model: to put structured input in front of the
// model, use WithJSONInput.
//
// Read it back with types.Message.Metadata. Calling WithMessageMetadata more
// than once in a call merges the maps, the later value winning for a repeated
// key. The values must be JSON-serializable, since state stores persist them
// as JSON, and they are kept in their decoded JSON form from the start: a
// number reads back as float64, a slice as []any and a struct as
// map[string]any, whether read live or from a store.
//
//	resp, err := conv.Send(ctx, "Align these",
//	    sdk.WithMessageMetadata(map[string]any{"selection": []string{"s1", "s2", "s3"}}))
func WithMessageMetadata(md map[string]any) SendOption {
	return func(c *sendConfig) error {
		if len(md) == 0 {
			return nil
		}
		// Keep the decoded JSON form, not the caller's values: it is what a
		// state store hands back, it shares nothing with the caller, and its
		// strings are all reachable by events.Redacting.
		raw, err := json.Marshal(md)
		if err != nil {
			return fmt.Errorf("WithMessageMetadata: metadata must be JSON-serializable: %w", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return fmt.Errorf("WithMessageMetadata: decode metadata: %w", err)
		}
		if c.messageMetadata == nil {
			c.messageMetadata = make(map[string]any, len(decoded))
		}
		maps.Copy(c.messageMetadata, decoded)
		return nil
	}
}

// withMessageMetadata returns the user message with the send's metadata
// attached under types.MetaMessageMetadata, merged over any the message
// already carries.
//
// It attaches to a shallow copy, never to msg itself: a *types.Message passed
// to Send belongs to the caller, who may reuse it for the next turn, and that
// turn must not inherit this one's metadata. The copy gets its own Meta map
// for the same reason.
func withMessageMetadata(msg *types.Message, md map[string]any) *types.Message {
	if len(md) == 0 {
		return msg
	}
	merged := msg.Metadata()
	if merged == nil {
		merged = make(map[string]any, len(md))
	}
	maps.Copy(merged, md)

	cp := *msg
	cp.Meta = make(map[string]interface{}, len(msg.Meta)+1)
	maps.Copy(cp.Meta, msg.Meta)
	cp.Meta[types.MetaMessageMetadata] = merged
	return &cp
}
