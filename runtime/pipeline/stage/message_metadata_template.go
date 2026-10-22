package stage

import (
	"encoding/json"
	"regexp"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// metadataPlaceholder matches a {{key}} placeholder in a message-metadata
// template, allowing spaces inside the braces.
var metadataPlaceholder = regexp.MustCompile(`{{\s*([^{}\s]+)\s*}}`)

// messageMetadataSeparator sits between a message's own text and the text
// rendered from its metadata.
const messageMetadataSeparator = "\n\n"

// messageMetadataTemplate returns the provider's message-metadata template, or
// "" when it has none.
func messageMetadataTemplate(p providers.Provider) string {
	if t, ok := p.(providers.MessageMetadataTemplater); ok {
		return t.MessageMetadataTemplate()
	}
	return ""
}

// withRenderedMessageMetadata returns messages with tmpl rendered from each
// message's application metadata (types.Message.Metadata) and appended to that
// message's text. It is applied to the request only: messages that change are
// copies, with their own Parts, so the conversation's stored history is never
// modified. An empty template, or a message whose rendering is empty, leaves
// the message as it was.
func withRenderedMessageMetadata(messages []types.Message, tmpl string) []types.Message {
	if tmpl == "" {
		return messages
	}
	var out []types.Message
	for i := range messages {
		md := messages[i].Metadata()
		if md == nil {
			continue
		}
		rendered := renderMessageMetadata(tmpl, md)
		if rendered == "" {
			continue
		}
		if out == nil {
			out = make([]types.Message, len(messages))
			copy(out, messages)
		}
		out[i] = appendMessageText(messages[i], messageMetadataSeparator+rendered)
	}
	if out == nil {
		return messages
	}
	return out
}

// appendMessageText returns a copy of m with text added after its own text:
// as a new text part when m has parts, otherwise on Content.
func appendMessageText(m types.Message, text string) types.Message {
	if len(m.Parts) > 0 {
		parts := make([]types.ContentPart, len(m.Parts), len(m.Parts)+1)
		copy(parts, m.Parts)
		parts = append(parts, types.NewTextPart(text))
		m.Parts = parts
		return m
	}
	m.Content += text
	return m
}

// renderMessageMetadata fills tmpl's {{key}} placeholders from md in a single
// pass, so a value that itself contains a placeholder is never expanded. A
// string is inserted as is and anything else as compact JSON. A key md does
// not carry, or a null value, renders as empty: one message without a field
// must not fail every later turn.
func renderMessageMetadata(tmpl string, md map[string]any) string {
	return metadataPlaceholder.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := metadataPlaceholder.FindStringSubmatch(match)[1]
		return metadataValueText(md[key])
	})
}

func metadataValueText(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
