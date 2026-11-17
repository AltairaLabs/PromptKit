package gemini

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A Live tool response is a list of function responses. On Gemini 3 and
// later Live models a function response takes images in its parts, as
// inline data (Gemini's multimodal function responses: PNG, JPEG and WebP),
// and the model sees them. Nothing else a tool returns can go there: audio
// in a function response closes the session (websocket 1007), and Gemini 2.x
// Live models ignore any media in one (observed live; a client_content turn
// or a realtime video frame carrying the image is not seen either). Media a
// function response cannot take, or that cannot be loaded, is described in
// its response instead (providers.UnsentToolMedia).

// apiNameLive names the API in a note about media a tool result could not
// send.
const apiNameLive = "Gemini Live"

// firstToolMediaMajor is the first Gemini major version whose Live models
// take media in a function response.
const firstToolMediaMajor = 3

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
	if !takesToolMedia(s.config.Model) {
		return nil, fmt.Sprintf("%s does not accept media in a Live tool result (Gemini 3 and later do)",
			baseModelName(s.config.Model))
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

// modelMajorVersion matches the major version in a Gemini model name, dotted
// or not: "gemini-3.8-flash", "gemini-3-flash-preview", "gemini-live-2.5-flash".
var modelMajorVersion = regexp.MustCompile(`(?:^|-)(\d+)(?:\.\d+)?(?:-|$)`)

// takesToolMedia reports whether a model takes media in a function
// response: Gemini 3 and later, Live or not. A model whose name carries no
// version (an alias such as "gemini-flash-latest", a tuned model) is not
// known to, so its tool media is described or sent after the round instead.
func takesToolMedia(model string) bool {
	m := modelMajorVersion.FindStringSubmatch(baseModelName(model))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1]) // digits only: an overflow reads as the largest int
	return major >= firstToolMediaMajor
}

// baseModelName is a model's name without a resource path
// ("projects/p/locations/l/publishers/google/models/gemini-3.8-live").
func baseModelName(model string) string {
	return model[strings.LastIndex(model, "/")+1:]
}
