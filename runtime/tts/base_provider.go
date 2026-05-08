package tts

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// The following methods satisfy the base.Provider interface for all three TTS
// service types (OpenAIService, ElevenLabsService, CartesiaService). They are
// defined here to avoid duplication across the three impl files.

// Type returns ProviderTypeTTS for all TTS services.
func (s *OpenAIService) Type() base.ProviderType { return base.ProviderTypeTTS }

// Validate performs synchronous config validation (no-op for TTS services).
func (s *OpenAIService) Validate() error { return nil }

// Init performs asynchronous setup (no-op for TTS services).
func (s *OpenAIService) Init(_ context.Context) error { return nil }

// HealthCheck reports liveness (no-op for TTS services).
func (s *OpenAIService) HealthCheck(_ context.Context) error { return nil }

// Close releases resources (no-op for TTS services; HTTP client is shared).
func (s *OpenAIService) Close() error { return nil }

// SynthesizeTTS implements base.TTSProvider for OpenAIService.
// It bridges the base.TTSRequest to the existing Synthesize method and wraps
// the response in a streaming ttsStream.
func (s *OpenAIService) SynthesizeTTS(ctx context.Context, req base.TTSRequest) (base.TTSStream, error) {
	cfg := SynthesisConfig{
		Voice: req.Voice,
		Speed: float64(req.Speed),
	}
	if req.Format != "" {
		cfg.Format = AudioFormat{Name: req.Format}
	} else {
		cfg.Format = FormatPCM16
	}
	reader, err := s.Synthesize(ctx, req.Text, cfg)
	if err != nil {
		return nil, err
	}
	return newReaderStream(reader, req.Text, s.Pricing(), s.ImplName()), nil
}

// --- ElevenLabsService ---

// Type returns ProviderTypeTTS for ElevenLabsService.
func (s *ElevenLabsService) Type() base.ProviderType { return base.ProviderTypeTTS }

// Validate performs synchronous config validation (no-op for TTS services).
func (s *ElevenLabsService) Validate() error { return nil }

// Init performs asynchronous setup (no-op for TTS services).
func (s *ElevenLabsService) Init(_ context.Context) error { return nil }

// HealthCheck reports liveness (no-op for TTS services).
func (s *ElevenLabsService) HealthCheck(_ context.Context) error { return nil }

// Close releases resources (no-op for TTS services; HTTP client is shared).
func (s *ElevenLabsService) Close() error { return nil }

// SynthesizeTTS implements base.TTSProvider for ElevenLabsService.
func (s *ElevenLabsService) SynthesizeTTS(ctx context.Context, req base.TTSRequest) (base.TTSStream, error) {
	cfg := SynthesisConfig{
		Voice: req.Voice,
		Speed: float64(req.Speed),
	}
	if req.Format != "" {
		cfg.Format = AudioFormat{Name: req.Format}
	} else {
		cfg.Format = FormatMP3
	}
	reader, err := s.Synthesize(ctx, req.Text, cfg)
	if err != nil {
		return nil, err
	}
	return newReaderStream(reader, req.Text, s.Pricing(), s.ImplName()), nil
}

// --- CartesiaService ---

// Type returns ProviderTypeTTS for CartesiaService.
func (s *CartesiaService) Type() base.ProviderType { return base.ProviderTypeTTS }

// Validate performs synchronous config validation (no-op for TTS services).
func (s *CartesiaService) Validate() error { return nil }

// Init performs asynchronous setup (no-op for TTS services).
func (s *CartesiaService) Init(_ context.Context) error { return nil }

// HealthCheck reports liveness (no-op for TTS services).
func (s *CartesiaService) HealthCheck(_ context.Context) error { return nil }

// Close releases resources (no-op for TTS services; HTTP client is shared).
func (s *CartesiaService) Close() error { return nil }

// SynthesizeTTS implements base.TTSProvider for CartesiaService.
func (s *CartesiaService) SynthesizeTTS(ctx context.Context, req base.TTSRequest) (base.TTSStream, error) {
	cfg := SynthesisConfig{
		Voice: req.Voice,
		Speed: float64(req.Speed),
	}
	if req.Format != "" {
		cfg.Format = AudioFormat{Name: req.Format}
	} else {
		cfg.Format = FormatPCM16
	}
	reader, err := s.Synthesize(ctx, req.Text, cfg)
	if err != nil {
		return nil, err
	}
	return newReaderStream(reader, req.Text, s.Pricing(), s.ImplName()), nil
}
