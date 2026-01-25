// Package media provides utilities for processing media content.
package media

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ContentConverter handles conversion of MediaContent to match provider requirements.
type ContentConverter struct {
	audioConverter *AudioConverter
}

// NewContentConverter creates a new content converter.
func NewContentConverter(config AudioConverterConfig) *ContentConverter {
	return &ContentConverter{
		audioConverter: NewAudioConverter(config),
	}
}

// ConvertMessageForProvider converts all media parts in a message to formats supported by the provider.
// Returns a new message with converted content (original is not modified).
func (c *ContentConverter) ConvertMessageForProvider(
	ctx context.Context,
	msg *types.Message,
	provider providers.Provider,
) (*types.Message, error) {
	if msg == nil || len(msg.Parts) == 0 {
		return msg, nil
	}

	// Get provider capabilities
	caps := getProviderCapabilities(provider)

	// Create a copy of the message
	newMsg := *msg
	newMsg.Parts = make([]types.ContentPart, len(msg.Parts))
	copy(newMsg.Parts, msg.Parts)

	// Convert each part if needed
	for i, part := range newMsg.Parts {
		converted, err := c.convertPartIfNeeded(ctx, part, caps)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part %d: %w", i, err)
		}
		newMsg.Parts[i] = converted
	}

	return &newMsg, nil
}

// ConvertMediaContentIfNeeded converts media content to a supported format if necessary.
func (c *ContentConverter) ConvertMediaContentIfNeeded(
	ctx context.Context,
	media *types.MediaContent,
	contentType string,
	targetFormats []string,
) (*types.MediaContent, error) {
	if media == nil || len(targetFormats) == 0 {
		return media, nil
	}

	// Check if already in supported format
	if IsFormatSupported(media.MIMEType, targetFormats) {
		return media, nil
	}

	// Select target format
	targetMIME := SelectTargetFormat(targetFormats)

	logger.Debug("Converting media format",
		"from", media.MIMEType,
		"to", targetMIME,
		"content_type", contentType,
	)

	switch contentType {
	case types.ContentTypeAudio:
		return c.convertAudioContent(ctx, media, targetMIME)
	// TODO: Add image and video conversion
	default:
		// Cannot convert, return as-is
		logger.Warn("No converter available for content type",
			"content_type", contentType,
			"from", media.MIMEType,
			"to", targetMIME,
		)
		return media, nil
	}
}

// convertPartIfNeeded converts a content part to a format supported by the provider.
func (c *ContentConverter) convertPartIfNeeded(
	ctx context.Context,
	part types.ContentPart,
	caps *providerCaps,
) (types.ContentPart, error) {
	if part.Media == nil {
		return part, nil
	}

	var targetFormats []string
	switch part.Type {
	case types.ContentTypeAudio:
		targetFormats = caps.audioFormats
	case types.ContentTypeImage:
		targetFormats = caps.imageFormats
	case types.ContentTypeVideo:
		targetFormats = caps.videoFormats
	default:
		return part, nil
	}

	if len(targetFormats) == 0 {
		// No format restrictions
		return part, nil
	}

	// Check if conversion is needed
	if IsFormatSupported(part.Media.MIMEType, targetFormats) {
		return part, nil
	}

	// Convert the content
	convertedMedia, err := c.ConvertMediaContentIfNeeded(ctx, part.Media, part.Type, targetFormats)
	if err != nil {
		return part, err
	}

	// Return new part with converted media
	newPart := part
	newPart.Media = convertedMedia
	return newPart, nil
}

// convertAudioContent converts audio content to the target format.
func (c *ContentConverter) convertAudioContent(
	ctx context.Context,
	media *types.MediaContent,
	targetMIME string,
) (*types.MediaContent, error) {
	// Get the raw audio data
	data, err := getMediaData(media)
	if err != nil {
		return nil, fmt.Errorf("failed to get audio data: %w", err)
	}

	// Perform conversion
	result, err := c.audioConverter.ConvertAudio(ctx, data, media.MIMEType, targetMIME)
	if err != nil {
		return nil, fmt.Errorf("audio conversion failed: %w", err)
	}

	// Create new media content with converted data
	encoded := base64.StdEncoding.EncodeToString(result.Data)
	return &types.MediaContent{
		Data:     &encoded,
		MIMEType: result.MIMEType,
	}, nil
}

// getMediaData extracts raw bytes from MediaContent.
func getMediaData(media *types.MediaContent) ([]byte, error) {
	if media.Data != nil && *media.Data != "" {
		return base64.StdEncoding.DecodeString(*media.Data)
	}
	// TODO: Handle URL and FilePath loading
	return nil, fmt.Errorf("no data available in MediaContent")
}

// providerCaps holds extracted provider capabilities.
type providerCaps struct {
	audioFormats []string
	imageFormats []string
	videoFormats []string
}

// getProviderCapabilities extracts capabilities from a provider.
func getProviderCapabilities(provider providers.Provider) *providerCaps {
	caps := &providerCaps{}

	// Check if provider implements MultimodalSupport
	if ms, ok := provider.(providers.MultimodalSupport); ok {
		mc := ms.GetMultimodalCapabilities()
		caps.audioFormats = mc.AudioFormats
		caps.imageFormats = mc.ImageFormats
		caps.videoFormats = mc.VideoFormats
	}

	return caps
}
