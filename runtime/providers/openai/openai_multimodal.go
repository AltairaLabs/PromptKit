package openai

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const audioPCM16Format = "pcm16"

// GetMultimodalCapabilities returns OpenAI's multimodal capabilities
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := providers.MultimodalCapabilities{
		SupportsImages: true,
		SupportsAudio:  false,
		SupportsVideo:  false,
		ImageFormats: []string{
			types.MIMETypeImageJPEG,
			types.MIMETypeImagePNG,
			types.MIMETypeImageGIF,
			types.MIMETypeImageWebP,
		},
		AudioFormats:   []string{},
		VideoFormats:   []string{},
		MaxImageSizeMB: 20, // OpenAI limit
		MaxAudioSizeMB: 0,
		MaxVideoSizeMB: 0,
	}

	// When the provider declares its capabilities, they are authoritative for
	// images/vision too; otherwise images stay on by default (above).
	if p.capabilities != nil {
		caps.SupportsImages = p.capabilities[providers.CapabilityVision]
	}

	// Audio models support audio input via both APIs: Chat Completions
	// (non-streaming + streaming) and Responses API (streaming events).
	if p.supportsAudioInput() {
		caps.SupportsAudio = true
		caps.AudioFormats = []string{
			types.MIMETypeAudioWAV,
			types.MIMETypeAudioMP3,
		}
		caps.MaxAudioSizeMB = 25 // OpenAI audio limit
	}

	return caps
}

// isAudioModel is the fallback heuristic used when a provider config does not
// declare its capabilities. Prefer the declared `capabilities` list; this only
// recognizes models by name for callers (e.g. some SDK usages) that omit it.
func isAudioModel(model string) bool {
	switch model {
	case "gpt-4o-audio-preview", "gpt-4o-mini-audio-preview",
		"gpt-audio-1.5", "gpt-audio-mini":
		return true
	default:
		return false
	}
}

// convertMessageToOpenAI converts a single PromptKit message to OpenAI format
func (p *Provider) convertMessageToOpenAI(ctx context.Context, msg types.Message) (openAIMessage, error) {
	// Handle legacy text-only messages
	if !msg.IsMultimodal() {
		return openAIMessage{
			Role:    msg.Role,
			Content: msg.GetContent(),
		}, nil
	}

	// Handle multimodal messages - OpenAI uses a different structure
	// Content becomes an array of content parts
	var contentParts []interface{}
	for _, part := range msg.Parts {
		element, err := p.convertContentPartToOpenAI(ctx, part)
		if err != nil {
			return openAIMessage{}, err
		}
		if element != nil {
			contentParts = append(contentParts, element)
		}
	}

	return openAIMessage{
		Role:    msg.Role,
		Content: contentParts,
	}, nil
}

// convertContentPartToOpenAI converts a single multimodal content part to its
// OpenAI representation. It returns (nil, nil) for parts that produce no output
// (e.g. empty text) so callers skip them.
func (p *Provider) convertContentPartToOpenAI(
	ctx context.Context, part types.ContentPart,
) (interface{}, error) {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text == nil || *part.Text == "" {
			return nil, nil
		}
		return map[string]interface{}{keyType: partTypeText, partTypeText: *part.Text}, nil

	case types.ContentTypeImage:
		if part.Media == nil {
			return nil, fmt.Errorf("image part missing media content")
		}
		return p.convertImagePartToOpenAI(ctx, part)

	case types.ContentTypeAudio:
		// Audio is only supported for audio models using Chat Completions API
		if p.apiMode != APIModeCompletions || !p.supportsAudioInput() {
			return nil, fmt.Errorf("audio content requires audio model with Chat Completions API")
		}
		if part.Media == nil {
			return nil, fmt.Errorf("audio part missing media content")
		}
		return p.convertAudioPartToOpenAI(ctx, part)

	case types.ContentTypeVideo:
		return nil, fmt.Errorf("video content not supported by OpenAI API")

	default:
		return nil, fmt.Errorf("unknown content type: %s", part.Type)
	}
}

// convertImagePartToOpenAI converts an image ContentPart to OpenAI's format
func (p *Provider) convertImagePartToOpenAI(
	ctx context.Context, part types.ContentPart,
) (map[string]interface{}, error) {
	if part.Media == nil {
		return nil, fmt.Errorf("image part missing media content")
	}

	imagePart := map[string]interface{}{
		"type": "image_url",
	}

	imageURL := make(map[string]interface{})

	// Prefer a model-fetchable URL (external URL or storage reference resolved via
	// the injected store); otherwise fall back to inline base64 bytes.
	loader := p.MediaLoader()
	if url, ok, err := loader.ResolveURL(ctx, part.Media); err != nil {
		return nil, fmt.Errorf("failed to resolve image: %w", err)
	} else if ok {
		imageURL["url"] = url
	} else {
		data, err := loader.GetBase64Data(ctx, part.Media)
		if err != nil {
			return nil, fmt.Errorf("failed to load image data: %w", err)
		}
		imageURL["url"] = fmt.Sprintf("data:%s;base64,%s", part.Media.MIMEType, data)
	}

	// Add detail level if specified
	if part.Media.Detail != nil {
		imageURL["detail"] = *part.Media.Detail
	}

	imagePart["image_url"] = imageURL

	return imagePart, nil
}

// convertAudioPartToOpenAI converts an audio ContentPart to OpenAI's input_audio format
// OpenAI audio input format: { "type": "input_audio", "input_audio": { "data": base64, "format": "wav"|"mp3" } }
func (p *Provider) convertAudioPartToOpenAI(
	ctx context.Context, part types.ContentPart,
) (map[string]interface{}, error) {
	if part.Media == nil {
		return nil, fmt.Errorf("audio part missing media content")
	}

	// Get base64-encoded audio data. The store-aware loader already returns inline
	// Data first, then resolves a StorageReference via the injected store.
	audioData, err := p.MediaLoader().GetBase64Data(ctx, part.Media)
	if err != nil {
		return nil, fmt.Errorf("failed to load audio data: %w", err)
	}

	// Determine audio format from MIME type
	format := getAudioFormat(part.Media.MIMEType)
	if format == "" {
		return nil, fmt.Errorf("unsupported audio format for OpenAI: %s (supported: wav, mp3)", part.Media.MIMEType)
	}

	return map[string]interface{}{
		"type": "input_audio",
		"input_audio": map[string]interface{}{
			"data":   audioData,
			"format": format,
		},
	}, nil
}

// getAudioFormat converts MIME type to OpenAI audio format string
func getAudioFormat(mimeType string) string {
	switch mimeType {
	case types.MIMETypeAudioWAV, "audio/x-wav":
		return "wav"
	case types.MIMETypeAudioMP3: // "audio/mpeg"
		return "mp3"
	default:
		return ""
	}
}

// audioFormatToMIME maps an OpenAI audio format string (from the audio.format
// request parameter or response) to a MIME type.
func audioFormatToMIME(format string) string {
	switch format {
	case "wav":
		return types.MIMETypeAudioWAV
	case "mp3":
		return types.MIMETypeAudioMP3
	case audioPCM16Format:
		return "audio/pcm"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/opus"
	default:
		return "application/octet-stream"
	}
}

// requestContainsAudio checks if any message in the request contains audio content
func requestContainsAudio(req *providers.PredictionRequest) bool {
	for i := range req.Messages {
		for j := range req.Messages[i].Parts {
			if req.Messages[i].Parts[j].Type == types.ContentTypeAudio {
				return true
			}
		}
	}
	return false
}
