package openai

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Chat Completions accepts only text in a tool message: "Image URLs are only
// allowed for messages with role 'user'". Media a tool returns therefore
// reaches the model in a user message that follows the round's tool results,
// converted exactly as a user's own media is. Media a user message cannot
// carry either (video, documents, audio on a model without audio input) is
// described in the tool message's text instead, and the replacement is logged.

// roleUser is the role of the message that carries a round's media.
const roleUser = "user"

// liftsToUserMessage reports whether a tool result's media part can be sent
// to the model in a user message.
func (p *ToolProvider) liftsToUserMessage(part *types.ContentPart) bool {
	if part.Media == nil {
		return false
	}
	switch part.Type {
	case types.ContentTypeImage:
		return true
	case types.ContentTypeAudio:
		return p.apiMode == APIModeCompletions && p.supportsAudioInput()
	default:
		return false
	}
}

// toolMessageText is what the tool message itself says: the result's text,
// then where its media went.
func (p *ToolProvider) toolMessageText(result *types.MessageToolResult) string {
	var notes []string
	lifted := 0
	var liftedKind string
	for i := range result.Parts {
		part := &result.Parts[i]
		if part.Type == types.ContentTypeText {
			continue
		}
		if p.liftsToUserMessage(part) {
			lifted++
			liftedKind = part.Type
			continue
		}
		notes = append(notes, unsentMediaNote(part, "OpenAI Chat Completions"))
		logger.Warn("tool result media not sent to the model: the API does not accept it",
			"provider", p.ID(), "call_id", result.ID, "tool", result.Name,
			"type", part.Type, "mime_type", mimeOf(part))
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

// liftedMediaMessage is the user message that carries the media of results,
// each result's media labeled with the tool and call it came from, or nil
// when none of them returned media a user message can carry.
func (p *ToolProvider) liftedMediaMessage(results []*types.MessageToolResult) *types.Message {
	var parts []types.ContentPart
	for _, result := range results {
		var media []types.ContentPart
		for i := range result.Parts {
			if result.Parts[i].Type != types.ContentTypeText && p.liftsToUserMessage(&result.Parts[i]) {
				media = append(media, result.Parts[i])
			}
		}
		if len(media) == 0 {
			continue
		}
		parts = append(parts, types.NewTextPart(
			fmt.Sprintf("Media returned by tool %s (call %s):", result.Name, result.ID)))
		parts = append(parts, media...)
	}
	if len(parts) == 0 {
		return nil
	}
	return &types.Message{Role: roleUser, Parts: parts}
}

// unsentMediaNote describes a media part a provider cannot take in a tool
// result, so the model knows the tool returned it and where it is.
func unsentMediaNote(part *types.ContentPart, api string) string {
	desc := part.Type
	if m := mimeOf(part); m != "" {
		desc += " (" + m
		if part.Media.StorageReference != nil && *part.Media.StorageReference != "" {
			desc += ", stored as " + *part.Media.StorageReference
		} else if part.Media.URL != nil && *part.Media.URL != "" {
			desc += ", at " + *part.Media.URL
		}
		desc += ")"
	}
	return fmt.Sprintf("[This tool returned %s %s that was not sent: %s does not accept %s input.]",
		article(part.Type), desc, api, part.Type)
}

func mimeOf(part *types.ContentPart) string {
	if part.Media == nil {
		return ""
	}
	return part.Media.MIMEType
}

func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// resolveToolResultMedia returns a copy of result whose images carry the URL
// or data URL they will be sent with, and whose documents carry their bytes,
// loaded through one MediaLoader so the result is held to the media caps.
// Other media is left as it is, to be described (responsesToolOutput).
func (p *Provider) resolveToolResultMedia(
	ctx context.Context, result *types.MessageToolResult,
) (*types.MessageToolResult, error) {
	loader := p.MediaLoader()
	resolved := *result
	resolved.Parts = make([]types.ContentPart, len(result.Parts))
	for i, part := range result.Parts {
		switch {
		case part.Media == nil:
		case part.Type == types.ContentTypeImage:
			converted, err := p.convertImagePartToOpenAI(ctx, loader, part)
			if err != nil {
				return nil, err
			}
			part.Media = resolvedImageMedia(part.Media, converted)
		case part.Type == types.ContentTypeDocument:
			data, err := loader.GetBase64Data(ctx, part.Media)
			if err != nil {
				return nil, fmt.Errorf("failed to load document data: %w", err)
			}
			media := *part.Media
			media.Data = &data
			part.Media = &media
		}
		resolved.Parts[i] = part
	}
	return &resolved, nil
}

// responsesToolOutput is a function_call_output's output: the result's text
// when it has no media, otherwise its parts in order as input_text,
// input_image and input_file items. Media the Responses API does not take
// (audio, video) becomes an input_text describing it, and is logged.
func (p *Provider) responsesToolOutput(result *types.MessageToolResult) any {
	if !result.HasMedia() {
		return result.GetTextContent()
	}
	items := make([]map[string]any, 0, len(result.Parts))
	for i := range result.Parts {
		part := &result.Parts[i]
		switch {
		case part.Type == types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				items = append(items, map[string]any{keyType: typeInputText, partTypeText: *part.Text})
			}
		case part.Type == types.ContentTypeImage && part.Media != nil:
			items = append(items, imageResponsesPart(part.Media))
		case part.Type == types.ContentTypeDocument && part.Media != nil && part.Media.Data != nil:
			items = append(items, map[string]any{
				keyType:     "input_file",
				"filename":  documentFilename(result, i),
				"file_data": "data:" + part.Media.MIMEType + ";base64," + *part.Media.Data,
			})
		default:
			note := unsentMediaNote(part, "OpenAI Responses")
			items = append(items, map[string]any{keyType: typeInputText, partTypeText: note})
			logger.Warn("tool result media not sent to the model: the API does not accept it",
				"provider", p.ID(), "call_id", result.ID, "tool", result.Name,
				"type", part.Type, "mime_type", mimeOf(part))
		}
	}
	return items
}

// documentFilename names the document at index i of result for the API,
// which requires a filename with file data: the tool, the call and the part's
// position, with the extension of its MIME type.
func documentFilename(result *types.MessageToolResult, i int) string {
	ext := ".bin"
	if exts, err := mime.ExtensionsByType(result.Parts[i].Media.MIMEType); err == nil && len(exts) > 0 {
		ext = exts[0]
	}
	return fmt.Sprintf("%s-%s-%d%s", result.Name, result.ID, i+1, ext)
}
