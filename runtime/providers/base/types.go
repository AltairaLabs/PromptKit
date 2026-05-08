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

// TTSProvider synthesizes audio from text.
type TTSProvider interface {
	Provider
	Synthesize(ctx context.Context, req TTSRequest) (TTSResponse, error)
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

// --- Request / response types for ancillary capabilities ---

// TTSRequest carries parameters for a text-to-speech synthesis call.
type TTSRequest struct {
	Text  string
	Voice string
	Speed float32
	Hints map[string]string // additional dimension hints (e.g. format, sample_rate)
}

// TTSResponse holds synthesized audio and metadata from a TTS provider.
type TTSResponse struct {
	Audio    []byte
	MIMEType string
	Cost     *types.CostInfo
	Latency  time.Duration
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
