package providers

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// UnsentToolMedia describes a tool result's media part that the provider's
// API does not accept in a tool result, so the model is told the tool
// returned it and where it is instead of the part vanishing, and logs the
// replacement. api names the API in the note ("Claude", "OpenAI Responses").
// A provider sends the returned text in the part's place.
func UnsentToolMedia(
	providerID, api string, result *types.MessageToolResult, part *types.ContentPart,
) string {
	mimeType := ""
	if part.Media != nil {
		mimeType = part.Media.MIMEType
	}
	logger.Warn("tool result media not sent to the model: the API does not accept it",
		"provider", providerID, "call_id", result.ID, "tool", result.Name,
		"type", part.Type, "mime_type", mimeType)
	desc := part.Type
	if mimeType != "" {
		desc += " (" + mimeType
		switch {
		case part.Media.StorageReference != nil && *part.Media.StorageReference != "":
			desc += ", stored as " + *part.Media.StorageReference
		case part.Media.URL != nil && *part.Media.URL != "":
			desc += ", at " + *part.Media.URL
		}
		desc += ")"
	}
	article := "a"
	if part.Type != "" && strings.ContainsRune("aeiou", rune(part.Type[0])) {
		article = "an"
	}
	return fmt.Sprintf("[This tool returned %s %s that was not sent: %s does not accept %s input.]",
		article, desc, api, part.Type)
}
