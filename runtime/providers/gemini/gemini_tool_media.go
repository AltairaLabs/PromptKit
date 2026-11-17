package gemini

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A tool result's media reaches the model as Gemini's multimodal function
// responses document: on Gemini 3 and later models, in the function
// response's parts, as inline data, in order. Gemini 2.x refuses media there
// (400 "Multimodal function responses are not supported for this model"), so
// on those models the media follows the round's function responses in a user
// content of its own, labeled with the tool and call, and the function
// response says it is attached there. Both observed live on gemini-2.5-flash
// and gemini-3.8-flash. Media Gemini does not take (a format or size over the
// limits) is described in the function response instead
// (providers.UnsentToolMedia). Media that cannot be loaded fails the request
// before it is sent, as on the other providers.

// apiNameGemini names the API in a note about media a tool result could not
// send.
const apiNameGemini = "Gemini"

// toolResultMedia returns a tool result's text, followed by a note for each
// media part not sent in its function response, the function response parts
// carrying its media (Gemini 3 and later), and the user content carrying it
// after the round (earlier models; nil when there is none).
func (p *Provider) toolResultMedia(
	ctx context.Context, result *types.MessageToolResult,
) (text string, parts []map[string]any, media map[string]any, err error) {
	caps := p.GetMultimodalCapabilities()
	loader := p.MediaLoader()
	var notes []string
	var kind string
	for i := range result.Parts {
		part := &result.Parts[i]
		if part.Type == types.ContentTypeText {
			continue
		}
		inline, why, err := p.toolMediaPart(ctx, loader, caps, part)
		if err != nil {
			return "", nil, nil, err
		}
		if why != "" {
			notes = append(notes, providers.UnsentToolMedia(p.ID(), result, part, why))
			continue
		}
		parts = append(parts, inline)
		kind = part.Type
	}
	if len(parts) > 0 && !takesToolMedia(p.model) {
		notes = append([]string{providers.AttachedToolMediaNote(len(parts), kind)}, notes...)
		label := map[string]any{wireKeyText: fmt.Sprintf("Media returned by tool %s (call %s):", result.Name, result.ID)}
		contentParts := append([]map[string]any{label}, parts...)
		media = map[string]any{wireKeyRole: roleUser, wireKeyParts: contentParts}
		parts = nil
	}
	return providers.JoinToolNotes(result.GetTextContent(), notes), parts, media, nil
}

// toolMediaPart converts a tool result's media part to inline data, or says
// why it cannot be sent; err is a load failure.
func (p *Provider) toolMediaPart(
	ctx context.Context, loader *providers.MediaLoader, caps providers.MultimodalCapabilities, part *types.ContentPart,
) (inline map[string]any, why string, err error) {
	if part.Media == nil {
		return nil, "it has no media content", nil
	}
	if why := providers.ToolMediaRefusal(apiNameGemini, caps, part); why != "" {
		return nil, why, nil
	}
	mimeType := mediaMIMEType(*part)
	if mimeType == "" {
		return nil, "its format is not stated", nil
	}
	data, err := loader.GetBase64Data(ctx, part.Media)
	if err != nil {
		return nil, "", fmt.Errorf("tool result %s: failed to load %s data: %w", part.Type, part.Type, err)
	}
	return map[string]any{wireKeyInlineData: map[string]any{
		wireKeyMimeType: mimeType,
		wireKeyData:     data,
	}}, "", nil
}
