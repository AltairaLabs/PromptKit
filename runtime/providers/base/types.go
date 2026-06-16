// Package base defines the unified provider abstraction shared across all
// PromptKit provider types (inference, TTS, STT, embedding, image generation).
//
// Every provider implements Provider for cross-cutting concerns
// (identity, pricing, lifecycle) and exactly one of the per-type interfaces
// below for capability-specific operations.
package base

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ProviderType discriminates capability types. Used as a registry key,
// metric label, and span attribute.
type ProviderType string

// Defined ProviderType values. New types should be added here and to AllProviderTypes.
const (
	ProviderTypeInference ProviderType = "inference"
	ProviderTypeTTS       ProviderType = "tts"
	ProviderTypeSTT       ProviderType = "stt"
	ProviderTypeEmbedding ProviderType = "embedding"
	ProviderTypeImage     ProviderType = "image"
	ProviderTypeVideo     ProviderType = "video"
)

// AllProviderTypes returns every defined ProviderType. Used by the metric
// collector to pre-register ancillary metric families.
func AllProviderTypes() []ProviderType {
	return []ProviderType{
		ProviderTypeInference,
		ProviderTypeTTS,
		ProviderTypeSTT,
		ProviderTypeEmbedding,
		ProviderTypeImage,
		ProviderTypeVideo,
	}
}

// ParseProviderType validates and returns a ProviderType from a string.
func ParseProviderType(s string) (ProviderType, error) {
	for _, t := range AllProviderTypes() {
		if string(t) == s {
			return t, nil
		}
	}
	return "", fmt.Errorf("unknown provider type %q", s)
}

// Provider is implemented by every provider type. It carries only the
// cross-cutting surface that the registry, config loader, metric collector,
// and cost rollup need.
type Provider interface {
	Name() string
	Type() ProviderType
	Pricing() *PricingDescriptor
	Validate() error
	Init(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	Close() error
}

// InferenceProvider is the shared interface for chat / realtime / multimodal
// LLM providers. The full surface (Predict, PredictStream, etc.) is defined
// in runtime/providers/provider.go and embedded here via the existing
// providers.Provider interface, kept intact for back-compat.
//
// For PR 1 this interface is forward-declared. Existing implementations
// satisfy it once they compose the base.Implementation helper (Task 7).
type InferenceProvider interface {
	Provider
}

// TTSStream is the streaming output of a TTS provider's Synthesize call.
// It exposes the chunk channel that downstream pipeline stages consume,
// plus a typed accessor for the total cost (available once the chunk
// channel closes) and a Close method for early cancellation.
type TTSStream interface {
	// Chunks returns the channel of audio chunks. Caller must drain to
	// completion or call Close to release resources. The channel closes
	// when synthesis completes successfully or on error.
	Chunks() <-chan audio.Chunk
	// Cost returns the total cost of this synthesis. Returns nil before
	// the chunk channel has closed; returns a populated *types.CostInfo
	// afterwards. Pricing comes from the provider's PricingDescriptor.
	Cost() *types.CostInfo
	// Close cancels an in-progress stream and releases resources.
	// Safe to call multiple times.
	Close() error
}

// TTSProvider produces audio from text. Streaming-first: every TTS call
// returns a TTSStream regardless of whether the underlying provider
// supports incremental synthesis. Buffered consumers can use ReadAllAudio
// to collect all chunks into a []byte.
//
// The method is named SynthesizeTTS rather than Synthesize to avoid a
// naming conflict with the legacy tts.Service.Synthesize method that takes
// (text string, config SynthesisConfig). Both interfaces can be satisfied
// by the same concrete type simultaneously.
type TTSProvider interface {
	Provider
	SynthesizeTTS(ctx context.Context, req TTSRequest) (TTSStream, error)
}

// STTProvider transcribes audio to text.
type STTProvider interface {
	Provider
	Transcribe(ctx context.Context, req STTRequest) (STTResponse, error)
}

// EmbeddingProvider returns vectors for input texts.
type EmbeddingProvider interface {
	Provider
	Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error)
}

// ImageProvider generates images from prompts.
type ImageProvider interface {
	Provider
	Generate(ctx context.Context, req ImageRequest) (ImageResponse, error)
}

// VideoProvider generates videos from prompts. No concrete provider implements
// this interface yet; it defines the shape that the video__generate tool
// resolves against once a provider (e.g. Veo) lands.
type VideoProvider interface {
	Provider
	Generate(ctx context.Context, req VideoRequest) (VideoResponse, error)
}

// --- Request / response types for ancillary capabilities ---

// TTSRequest carries parameters for a text-to-speech synthesis call.
type TTSRequest struct {
	Text       string
	Voice      string
	Speed      float32
	Format     string            // audio format hint, e.g. "mp3", "pcm" (provider-specific)
	SampleRate int               // desired output sample rate in Hz (0 = provider default)
	Hints      map[string]string // additional dimension hints passed to pricing
}

// ReadAllAudio drains a TTSStream into a byte slice and returns it along
// with the total cost. Convenience for callers that need the full audio
// up front (tests, batch synthesis, mocks).
func ReadAllAudio(stream TTSStream) ([]byte, *types.CostInfo, error) {
	defer func() { _ = stream.Close() }()
	var buf []byte
	for chunk := range stream.Chunks() {
		if chunk.Error != nil {
			return nil, nil, chunk.Error
		}
		buf = append(buf, chunk.Data...)
	}
	return buf, stream.Cost(), nil
}

// STTRequest carries audio input for a speech-to-text transcription call.
type STTRequest struct {
	Audio    []byte
	MIMEType string
	Hints    map[string]string
}

// STTResponse holds the transcribed text and metadata from an STT provider.
type STTResponse struct {
	Text    string
	Cost    *types.CostInfo
	Latency time.Duration
}

// EmbeddingRequest carries text inputs for a vector embedding call.
type EmbeddingRequest struct {
	Inputs []string
	Hints  map[string]string
}

// EmbeddingResponse holds the embedding vectors and metadata from a provider.
type EmbeddingResponse struct {
	Vectors [][]float32
	Cost    *types.CostInfo
	Latency time.Duration
}

// ImageRequest carries parameters for an image generation call.
type ImageRequest struct {
	Prompt  string
	Size    string // e.g. "1024x1024"
	Quality string // e.g. "standard" | "hd"
	Count   int    // default 1
	Hints   map[string]string
}

// ImageResponse holds generated image bytes and metadata from a provider.
type ImageResponse struct {
	Images   [][]byte
	MIMEType string
	Cost     *types.CostInfo
	Latency  time.Duration
}

// VideoRequest carries parameters for a video generation call.
type VideoRequest struct {
	Prompt      string
	AspectRatio string            // e.g. "16:9"
	Hints       map[string]string // additional dimension hints passed to pricing
}

// VideoResponse holds generated video bytes and metadata from a provider.
type VideoResponse struct {
	Videos   [][]byte
	MIMEType string // e.g. "video/mp4"
	Cost     *types.CostInfo
	Latency  time.Duration
}
