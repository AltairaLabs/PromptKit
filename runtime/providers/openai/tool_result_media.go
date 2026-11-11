package openai

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Names of the two APIs in a note about media a tool result could not send.
const (
	apiNameChat      = "OpenAI Chat Completions"
	apiNameResponses = "OpenAI Responses"
)

// roleUser is the role of a message that carries a tool result's media.
const roleUser = "user"

// Chat Completions accepts only text in a tool message: "Image URLs are only
// allowed for messages with role 'user'". Media a tool returns therefore
// reaches the model in a user message that follows the round's tool results,
// one message per result, converted exactly as a user's own media is. Media a
// user message cannot carry (video, documents, audio on a model without audio
// input, a format or size over the limits) is described in the tool message
// instead (providers.UnsentToolMedia).

// liftsToUserMessage reports whether a tool result's media part is sent to
// the model in a user message.
func (p *ToolProvider) liftsToUserMessage(part *types.ContentPart) bool {
	return part.Type != types.ContentTypeText && part.Media != nil &&
		providers.ToolMediaRefusal(apiNameChat, p.SendableMultimodalCapabilities(), part) == ""
}

// toolMessageText is what the tool message itself says: the result's text,
// then where its media went or why it was not sent.
func (p *ToolProvider) toolMessageText(result *types.MessageToolResult) string {
	caps := p.SendableMultimodalCapabilities()
	var notes []string
	lifted := 0
	var liftedKind string
	for i := range result.Parts {
		part := &result.Parts[i]
		if part.Type == types.ContentTypeText || part.Media == nil {
			continue
		}
		if why := providers.ToolMediaRefusal(apiNameChat, caps, part); why != "" {
			notes = append(notes, providers.UnsentToolMedia(p.ID(), result, part, why))
			continue
		}
		lifted++
		liftedKind = part.Type
	}
	switch {
	case lifted == 1:
		notes = append([]string{fmt.Sprintf(
			"[The %s this tool returned is attached in the next user message.]", liftedKind)}, notes...)
	case lifted > 1:
		notes = append([]string{fmt.Sprintf(
			"[The %d media parts this tool returned are attached in the next user message.]", lifted)}, notes...)
	}
	text := result.GetTextContent()
	if len(notes) == 0 {
		return text
	}
	if text == "" {
		return strings.Join(notes, "\n")
	}
	return text + "\n" + strings.Join(notes, "\n")
}

// liftedMediaMessages are the user messages that carry the media of results,
// one per result that returned media a user message can carry, each labeled
// with the tool and call it came from.
func (p *ToolProvider) liftedMediaMessages(results []*types.MessageToolResult) []types.Message {
	var out []types.Message
	for _, result := range results {
		var media []types.ContentPart
		for i := range result.Parts {
			if p.liftsToUserMessage(&result.Parts[i]) {
				media = append(media, result.Parts[i])
			}
		}
		if len(media) == 0 {
			continue
		}
		parts := append([]types.ContentPart{types.NewTextPart(
			fmt.Sprintf("Media returned by tool %s (call %s):", result.Name, result.ID))}, media...)
		out = append(out, types.Message{Role: roleUser, Parts: parts})
	}
	return out
}

// responsesToolCaps is what a Responses API function_call_output takes:
// images as on a user message, and PDF documents as input_file.
func (p *Provider) responsesToolCaps() providers.MultimodalCapabilities {
	caps := p.SendableMultimodalCapabilities()
	caps.SupportsDocuments = true
	caps.DocumentFormats = []string{types.MIMETypePDF}
	return caps
}

// resolveToolResultMedia returns a copy of result whose images carry the URL
// or data URL they will be sent with, and whose documents carry their bytes,
// loaded through one MediaLoader so the result is held to the media caps.
// Media the API does not take is left as it is, to be described
// (responsesToolOutput).
func (p *Provider) resolveToolResultMedia(
	ctx context.Context, result *types.MessageToolResult,
) (*types.MessageToolResult, error) {
	caps := p.responsesToolCaps()
	loader := p.MediaLoader()
	resolved := *result
	resolved.Parts = make([]types.ContentPart, len(result.Parts))
	for i, part := range result.Parts {
		resolved.Parts[i] = part
		if part.Media == nil || providers.ToolMediaRefusal(apiNameResponses, caps, &part) != "" {
			continue
		}
		switch part.Type {
		case types.ContentTypeImage:
			converted, err := p.convertImagePartToOpenAI(ctx, loader, part)
			if err != nil {
				return nil, err
			}
			resolved.Parts[i].Media = resolvedImageMedia(part.Media, converted)
		case types.ContentTypeDocument:
			data, err := loader.GetBase64Data(ctx, part.Media)
			if err != nil {
				return nil, fmt.Errorf("failed to load document data: %w", err)
			}
			media := *part.Media
			media.Data = &data
			resolved.Parts[i].Media = &media
		}
	}
	return &resolved, nil
}

// responsesToolOutput is a function_call_output's output: the result's text
// when it has no media, otherwise its parts in order as input_text,
// input_image and input_file items. Media the Responses API does not take
// becomes an input_text describing it (providers.UnsentToolMedia).
func (p *Provider) responsesToolOutput(result *types.MessageToolResult) any {
	if !result.HasMedia() {
		return result.GetTextContent()
	}
	caps := p.responsesToolCaps()
	items := make([]map[string]any, 0, len(result.Parts))
	for i := range result.Parts {
		part := &result.Parts[i]
		if part.Type == types.ContentTypeText {
			if part.Text != nil && *part.Text != "" {
				items = append(items, map[string]any{keyType: typeInputText, partTypeText: *part.Text})
			}
			continue
		}
		if part.Media == nil {
			continue
		}
		if why := providers.ToolMediaRefusal(apiNameResponses, caps, part); why != "" {
			note := providers.UnsentToolMedia(p.ID(), result, part, why)
			items = append(items, map[string]any{keyType: typeInputText, partTypeText: note})
			continue
		}
		if part.Type == types.ContentTypeImage {
			items = append(items, imageResponsesPart(part.Media))
			continue
		}
		items = append(items, map[string]any{
			keyType:     "input_file",
			"filename":  fmt.Sprintf("%s-%s-%d.pdf", result.Name, result.ID, i+1),
			"file_data": "data:" + part.Media.MIMEType + ";base64," + *part.Media.Data,
		})
	}
	return items
}
