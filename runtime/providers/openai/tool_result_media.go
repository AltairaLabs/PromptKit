package openai

import (
	"fmt"
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
