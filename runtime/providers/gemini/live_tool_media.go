package gemini

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A Live tool response is a list of function responses. A function response
// takes images in its parts, as inline data (Gemini's multimodal function
// responses: PNG, JPEG and WebP), and the model sees them. Nothing else a
// tool returns can go there: audio in a function response closes the session
// (websocket 1007). Gemini 2.x Live models ignore any media in one, without
// an error (observed live; a client_content turn or a realtime video frame
// carrying the image is not seen either), so the session cannot learn it: a
// provider on such a model is configured with
// additional_config.function_response_media: false
// (StreamSessionConfig.NoFunctionResponseMedia), and the media is described
// instead. Media a function response cannot take, or that cannot be loaded,
// is described in its response too (providers.UnsentToolMedia).

// apiNameLive names the API in a note about media a tool result could not
// send.
const apiNameLive = "Gemini Live"

// liveToolImageCaps is what a Live function response's parts take.
var liveToolImageCaps = providers.MultimodalCapabilities{
	SupportsImages: true,
	ImageFormats:   []string{types.MIMETypeImagePNG, types.MIMETypeImageJPEG, types.MIMETypeImageWebP},
}

// SupportsToolResponseParts reports that the session sends a tool result's
// media (providers.ToolResponse.Parts) where the Live API takes it and
// describes it where it does not. Implements
// providers.ToolResponsePartsSupport.
func (s *StreamSession) SupportsToolResponseParts() bool { return true }

// toolResponseWire is the toolResponse message for responses: each one's
// text, with a note for each media part not sent, and its images as parts.
func (s *StreamSession) toolResponseWire(ctx context.Context, responses []providers.ToolResponse) map[string]any {
	out := make([]providers.ToolResponse, len(responses))
	parts := make([][]map[string]any, len(responses))
	var loader *providers.MediaLoader
	for i := range responses {
		out[i] = responses[i]
		if !(&types.MessageToolResult{Parts: responses[i].Parts}).HasMedia() {
			continue
		}
		if loader == nil {
			loader = s.toolMediaLoader()
		}
		out[i].Result, parts[i] = s.liveToolMedia(ctx, loader, &responses[i])
	}
	return toolResponseMessage(out, parts)
}

// toolMediaLoader is the loader one tool round's media is read through.
func (s *StreamSession) toolMediaLoader() *providers.MediaLoader {
	if s.config.mediaLoader != nil {
		return s.config.mediaLoader()
	}
	return providers.NewMediaLoader(providers.MediaLoaderConfig{})
}

// liveToolMedia returns a response's text, followed by a note for each media
// part not sent, and the function response parts of those sent.
func (s *StreamSession) liveToolMedia(
	ctx context.Context, loader *providers.MediaLoader, resp *providers.ToolResponse,
) (string, []map[string]any) {
	result := &types.MessageToolResult{ID: resp.ToolCallID, Parts: resp.Parts}
	var notes []string
	var parts []map[string]any
	for i := range resp.Parts {
		part := &resp.Parts[i]
		if part.Type == types.ContentTypeText {
			continue
		}
		inline, why := s.liveToolPart(ctx, loader, part)
		if why != "" {
			notes = append(notes, providers.UnsentToolMedia(s.providerID, result, part, why))
			continue
		}
		parts = append(parts, inline)
	}
	return providers.JoinToolNotes(resp.Result, notes), parts
}

// liveToolPart converts a tool result's media part to a function response
// part, or says why it cannot be sent.
func (s *StreamSession) liveToolPart(
	ctx context.Context, loader *providers.MediaLoader, part *types.ContentPart,
) (map[string]any, string) {
	if part.Media == nil {
		return nil, whyNoMediaContent
	}
	if s.config.NoFunctionResponseMedia {
		return nil, fmt.Sprintf("%s is configured to take no media in a tool result (additional_config.%s)",
			apiNameLive, functionResponseMediaKey)
	}
	if part.Type != types.ContentTypeImage {
		return nil, fmt.Sprintf("%s does not accept %s in a tool result", apiNameLive, part.Type)
	}
	if why := providers.ToolMediaRefusal(apiNameLive, liveToolImageCaps, part); why != "" {
		return nil, why
	}
	if part.Media.MIMEType == "" {
		return nil, "its image format is not stated"
	}
	data, err := loader.GetBase64Data(ctx, part.Media)
	if err != nil {
		return nil, "it could not be loaded: " + err.Error()
	}
	return map[string]any{wireKeyInlineData: map[string]any{
		wireKeyMimeType: part.Media.MIMEType,
		wireKeyData:     data,
	}}, ""
}
