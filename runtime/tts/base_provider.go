package tts

import (
	"context"
	"fmt"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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
	if err := applyRequestedSampleRate(&cfg, req, nil); err != nil {
		return nil, err
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
	if err := applyRequestedSampleRate(&cfg, req, elevenLabsPCMSampleRates); err != nil {
		return nil, err
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
	if err := applyRequestedSampleRate(&cfg, req, cartesiaPCMSampleRates); err != nil {
		return nil, err
	}
	reader, err := s.Synthesize(ctx, req.Text, cfg)
	if err != nil {
		return nil, err
	}
	return newReaderStream(reader, req.Text, s.Pricing(), s.ImplName()), nil
}

// applyRequestedSampleRate carries TTSRequest.SampleRate onto the synthesis
// format. Raw PCM carries no header naming its rate, so a service that honors
// Format.SampleRate for PCM must be told the rate the caller will play it at;
// zero keeps the format's default. A PCM rate outside pcmRates (when the
// service lists them) is refused here, naming the rates it takes, rather than
// sent for the vendor to reject.
func applyRequestedSampleRate(cfg *SynthesisConfig, req base.TTSRequest, pcmRates []int) error {
	if req.SampleRate <= 0 {
		return nil
	}
	if pcmRates != nil && cfg.Format.Name == formatPCM && !slices.Contains(pcmRates, req.SampleRate) {
		return fmt.Errorf("tts: PCM sample rate %d Hz is not supported (supported: %v)", req.SampleRate, pcmRates)
	}
	cfg.Format.SampleRate = req.SampleRate
	return nil
}

// PCM output rates each vendor's API accepts for raw PCM.
var (
	elevenLabsPCMSampleRates = []int{8000, 16000, 22050, 24000, 44100, 48000}
	cartesiaPCMSampleRates   = []int{8000, 16000, 22050, 24000, 44100}
)
