package gemini

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A tool result's media reaches the model as Gemini's multimodal function
// responses document: in the function response's parts, as inline data, in
// order. A model whose function responses take no media refuses that request
// (400 "Multimodal function responses are not supported for this model":
// gemini-2.5-flash; gemini-3.8-flash and gemini-flash-latest take it, observed
// live), and for it the media follows the round's function responses in a
// user content of its own, labeled with the tool and call, and the function
// response says it is attached there. Which one a model is comes from
// additional_config.function_response_media when it is set; otherwise the
// provider sends the media in the function response and, on that refusal,
// sends the request again the other way and keeps doing so. Media Gemini
// does not take (a format or size over the limits) is described in the
// function response instead (providers.UnsentToolMedia). Media that cannot be
// loaded fails the request before it is sent, as on the other providers.

// apiNameGemini names the API in a note about media a tool result could not
// send.
const apiNameGemini = "Gemini"

// whyNoMediaContent is why a media part with no media content is not sent.
const whyNoMediaContent = "it has no media content"

// toolResultMedia returns a tool result's text, followed by a note for each
// media part not sent in its function response, the function response parts
// carrying its media, and the user content carrying it after the round
// instead, for a model whose function responses take none (nil otherwise).
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
	if len(parts) > 0 && !p.toolMedia.inFunctionResponse() {
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
		return nil, whyNoMediaContent, nil
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

// functionResponseMediaKey is the additional_config key declaring whether the
// model's function responses take media.
const functionResponseMediaKey = "function_response_media"

// multimodalFunctionResponseRefusal is the message of Gemini's 400 for media
// in a function response on a model that takes none.
const multimodalFunctionResponseRefusal = "Multimodal function responses are not supported"

// functionResponseMedia is whether a provider's model takes media in a
// function response: declared, else refused by the API, else assumed. A nil
// one (a Provider built without a constructor) takes it and learns nothing.
type functionResponseMedia struct {
	declared *bool
	refused  atomic.Bool
}

// inFunctionResponse reports whether a tool result's media goes in its
// function response.
func (m *functionResponseMedia) inFunctionResponse() bool {
	if m == nil {
		return true
	}
	if m.declared != nil {
		return *m.declared
	}
	return !m.refused.Load()
}

// declaredOff reports whether the config declares that the model's function
// responses take no media.
func (m *functionResponseMedia) declaredOff() bool {
	return m != nil && m.declared != nil && !*m.declared
}

// learn records that the model takes no media in a function response when err
// is the API's refusal of it, and reports whether it did: the request is
// worth sending again. A declared value is never second-guessed.
func (m *functionResponseMedia) learn(err error, providerID, model string) bool {
	if err == nil || m == nil || m.declared != nil || m.refused.Load() ||
		!strings.Contains(err.Error(), multimodalFunctionResponseRefusal) {
		return false
	}
	m.refused.Store(true)
	logger.Warn("gemini: the model takes no media in a function response; sending tool media after the round "+
		"(set additional_config."+functionResponseMediaKey+": false to skip the first attempt)",
		"provider", providerID, "model", model)
	return true
}

// withToolMediaFallback runs call, and when the API refuses the media its
// function responses carried, runs it once more with the media after the
// round.
func withToolMediaFallback[T any](p *Provider, call func() (T, error)) (T, error) {
	out, err := call()
	if !p.toolMedia.learn(err, p.ID(), p.model) {
		return out, err
	}
	return call()
}

// applyFunctionResponseMediaConfig reads additional_config.function_response_media.
func applyFunctionResponseMediaConfig(p *Provider, spec providers.ProviderSpec) error {
	raw, set := spec.AdditionalConfig[functionResponseMediaKey]
	if !set {
		return nil
	}
	v, ok := raw.(bool)
	if !ok {
		return fmt.Errorf("gemini provider %q: additional_config.%s must be true or false, got %q",
			spec.ID, functionResponseMediaKey, fmt.Sprint(raw))
	}
	p.toolMedia = &functionResponseMedia{declared: &v}
	return nil
}
