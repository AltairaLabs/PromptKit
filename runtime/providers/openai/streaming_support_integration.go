// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// CreateStreamSession creates a new bidirectional streaming session with OpenAI Realtime API.
//
// The session supports real-time audio input/output with the following features:
// - Bidirectional audio streaming (send and receive audio simultaneously)
// - Server-side voice activity detection (VAD) for automatic turn detection
// - Function/tool calling during the streaming session
// - Input and output audio transcription
//
// Audio Format:
// OpenAI Realtime API uses 24kHz 16-bit PCM mono audio by default.
// The session automatically handles base64 encoding/decoding of audio data.
//
// Example usage:
//
//	session, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{
//	    Config: types.StreamingMediaConfig{
//	        Type:       types.ContentTypeAudio,
//	        SampleRate: 24000,
//	        Encoding:   "pcm16",
//	        Channels:   1,
//	    },
//	    SystemInstruction: "You are a helpful assistant.",
//	})
func (p *Provider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	if err := p.validateStreamRequest(req); err != nil {
		return nil, err
	}

	config := p.buildRealtimeSessionConfig(req)
	p.applyStreamMetadata(req.Metadata, &config)
	p.applyStreamTools(req.Tools, &config)

	session, err := NewRealtimeSession(ctx, p.apiKey, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to create realtime session: %w", err)
	}

	return session, nil
}
