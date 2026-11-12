package providers

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A tool result's media is sent to the model in whatever form the provider's
// API takes for a tool result. A part it cannot send is never dropped and
// never fails the request: the request is rebuilt from history on every later
// turn, so one bad part would fail the conversation for good. The provider
// sends a note in its place (UnsentToolMedia) saying what the tool returned
// and why it was not sent (ToolMediaRefusal).

// ToolMediaRefusal returns why a tool result's media part cannot be sent to an
// API whose tool results take what caps describe, or "" when it can: its kind
// is not taken, its format is not one of the kind's formats, or its inline or
// declared size is over the kind's limit. api names the API in the reason.
func ToolMediaRefusal(api string, caps MultimodalCapabilities, part *types.ContentPart) string {
	supported, formats, maxMB := kindLimits(caps, part.Type)
	if !supported {
		return fmt.Sprintf("%s does not accept %s input", api, part.Type)
	}
	if part.Media == nil {
		return ""
	}
	if mt := part.Media.MIMEType; mt != "" && len(formats) > 0 && !slices.Contains(formats, mt) {
		return fmt.Sprintf("%s does not accept %s %ss", api, mt, part.Type)
	}
	if sizeMB, known := inlineSizeMB(part.Media); known && maxMB > 0 && sizeMB > float64(maxMB) {
		return fmt.Sprintf("it is %.1f MB, over %s %d MB limit for %ss", sizeMB, possessive(api), maxMB, part.Type)
	}
	return ""
}

// UnsentToolMedia is the note a provider sends in place of a tool result's
// media part it cannot send: the part's kind, MIME type and storage reference
// or URL (with credentials in the URL redacted), and why, from
// ToolMediaRefusal. The replacement is logged at debug: the note is rebuilt on
// every later request, and the note itself is what the model and the
// transcript see.
func UnsentToolMedia(providerID string, result *types.MessageToolResult, part *types.ContentPart, why string) string {
	mimeType := ""
	if part.Media != nil {
		mimeType = part.Media.MIMEType
	}
	logger.Debug("tool result media not sent to the model",
		"provider", providerID, "call_id", result.ID, "tool", result.Name,
		"type", part.Type, "mime_type", mimeType, "reason", why)
	desc := part.Type
	if mimeType != "" {
		desc += " (" + mimeType
		switch {
		case part.Media.StorageReference != nil && *part.Media.StorageReference != "":
			desc += ", stored as " + *part.Media.StorageReference
		case part.Media.URL != nil && *part.Media.URL != "":
			desc += ", at " + RedactURLSecrets(*part.Media.URL)
		}
		desc += ")"
	}
	article := "a"
	if part.Type != "" && strings.ContainsRune("aeiou", rune(part.Type[0])) {
		article = "an"
	}
	return fmt.Sprintf("[This tool returned %s %s that was not sent: %s.]", article, desc, why)
}

// possessive is name with its possessive ending: "Claude's", "OpenAI Responses'".
func possessive(name string) string {
	if strings.HasSuffix(name, "s") {
		return name + "'"
	}
	return name + "'s"
}

// kindLimits returns whether caps takes media of kind, its formats and its
// size limit in MB (0: none declared).
func kindLimits(caps MultimodalCapabilities, kind string) (supported bool, formats []string, maxMB int) {
	switch kind {
	case types.ContentTypeImage:
		return caps.SupportsImages, caps.ImageFormats, caps.MaxImageSizeMB
	case types.ContentTypeAudio:
		return caps.SupportsAudio, caps.AudioFormats, caps.MaxAudioSizeMB
	case types.ContentTypeVideo:
		return caps.SupportsVideo, caps.VideoFormats, caps.MaxVideoSizeMB
	case types.ContentTypeDocument:
		return caps.SupportsDocuments, caps.DocumentFormats, caps.MaxDocumentSizeMB
	}
	return false, nil, 0
}

// inlineSizeMB is the decoded size of media in MB when it is known without
// loading anything: its declared SizeKB, or the length of its inline data.
func inlineSizeMB(media *types.MediaContent) (float64, bool) {
	const kbPerMB, bytesPerMB = 1024, 1024 * 1024
	switch {
	case media.SizeKB != nil:
		return float64(*media.SizeKB) / kbPerMB, true
	case media.Data != nil:
		return float64(base64.StdEncoding.DecodedLen(len(*media.Data))) / bytesPerMB, true
	}
	return 0, false
}

// ToolResultTextWithUnsentMedia is the text of a tool message for an API
// whose tool messages take text only and that sends a tool's media nowhere
// else: the result's text, then a note (UnsentToolMedia) for each media part,
// so the model knows what the tool returned. api names the API in the notes.
func ToolResultTextWithUnsentMedia(providerID, api string, result *types.MessageToolResult) string {
	text := result.GetTextContent()
	why := api + " does not send media in a tool result"
	var notes []string
	for i := range result.Parts {
		part := &result.Parts[i]
		if part.Type == types.ContentTypeText || part.Media == nil {
			continue
		}
		notes = append(notes, UnsentToolMedia(providerID, result, part, why))
	}
	if len(notes) == 0 {
		return text
	}
	if text == "" {
		return strings.Join(notes, "\n")
	}
	return text + "\n" + strings.Join(notes, "\n")
}
