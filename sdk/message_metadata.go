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
// as JSON; after a round trip through a store, numbers read back as float64.
//
//	resp, err := conv.Send(ctx, "Align these",
//	    sdk.WithMessageMetadata(map[string]any{"selection": []string{"s1", "s2", "s3"}}))
func WithMessageMetadata(md map[string]any) SendOption {
	return func(c *sendConfig) error {
		if len(md) == 0 {
			return nil
		}
		if _, err := json.Marshal(md); err != nil {
			return fmt.Errorf("WithMessageMetadata: metadata must be JSON-serializable: %w", err)
		}
		if c.messageMetadata == nil {
			c.messageMetadata = make(map[string]any, len(md))
		}
		maps.Copy(c.messageMetadata, md)
		return nil
	}
}

// applyMessageMetadata attaches the send's metadata to the user message under
// types.MetaMessageMetadata, merged over any the message already carries.
//
// A *types.Message passed to Send is used as is, so like the content-part
// options this sets the metadata on it. It replaces the message's Meta map
// rather than writing into it: the caller may share that map with other
// messages, and they must not gain this turn's metadata.
func applyMessageMetadata(msg *types.Message, md map[string]any) {
	if len(md) == 0 {
		return
	}
	merged := maps.Clone(msg.Metadata())
	if merged == nil {
		merged = make(map[string]any, len(md))
	}
	maps.Copy(merged, md)

	meta := make(map[string]interface{}, len(msg.Meta)+1)
	maps.Copy(meta, msg.Meta)
	meta[types.MetaMessageMetadata] = merged
	msg.Meta = meta
}
