package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Compile-time check: ElevenLabsService satisfies Service.
var _ Service = (*ElevenLabsService)(nil)

const (
	elevenLabsBaseURL            = "https://api.elevenlabs.io/v1"
	elevenLabsTranscribeEndpoint = "/speech-to-text"
	providerElevenLabs           = "elevenlabs"

	// ElevenLabsModelScribeV3 is the ElevenLabs Scribe v3 batch transcription
	// model, the one ElevenLabsService uses by default.
	ElevenLabsModelScribeV3 = "scribe_v3"

	defaultElevenLabsTimeout = 60 * time.Second

	// elevenLabsRawPCMFormat is the file_format value for raw 16-bit
	// little-endian mono PCM at 16 kHz, the only raw encoding the endpoint
	// takes. Other PCM is wrapped as WAV.
	elevenLabsRawPCMFormat = "pcm_s16le_16"
	elevenLabsRawPCMRate   = 16000

	// elevenLabsPerSecondRate is ElevenLabs' published Scribe API rate,
	// $0.20 per hour of audio.
	elevenLabsPerSecondRate = 0.20 / secondsPerHour
)

// elevenLabsDefaultPricing is the inline fallback price for Scribe: $0.20 per
// hour of audio, billed per second.
var elevenLabsDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: currencyUSD,
	Items:    []base.PriceItem{{Unit: unitSecond, Rate: elevenLabsPerSecondRate}},
}

// ElevenLabsService implements STT with the ElevenLabs Speech-to-Text (Scribe)
// batch API.
type ElevenLabsService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
}

// ElevenLabsOption configures the ElevenLabs STT service. It is a type alias
// for base.HTTPServiceOption, so base.WithBaseURL, base.WithClient and
// base.WithModel apply directly.
type ElevenLabsOption = base.HTTPServiceOption

// NewElevenLabs creates an ElevenLabs STT service. The model defaults to
// ElevenLabsModelScribeV3.
func NewElevenLabs(apiKey string, opts ...ElevenLabsOption) *ElevenLabsService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    providerElevenLabs,
		Type:    base.ProviderTypeSTT,
		Pricing: elevenLabsDefaultPricing,
		BaseURL: elevenLabsBaseURL,
		Model:   ElevenLabsModelScribeV3,
		Timeout: defaultElevenLabsTimeout,
	}, opts...)
	return &ElevenLabsService{Implementation: impl, HTTPServiceFields: fields}
}

// Transcribe implements base.STTProvider. The cost prices the audio duration
// the response reports, or, when it reports none, the length of PCM/WAV input.
func (s *ElevenLabsService) Transcribe(ctx context.Context, req base.STTRequest) (base.STTResponse, error) {
	return transcribeWithCost(ctx, s.Implementation, s.Model, req, s.transcribe)
}

// TranscribeBytes converts audio to text and returns the transcript only, with
// no cost. config.Prompt is not sent.
func (s *ElevenLabsService) TranscribeBytes(
	ctx context.Context, audio []byte, config TranscriptionConfig,
) (string, error) {
	return transcribeBytes(ctx, audio, config, s.Model, s.transcribe)
}

// elevenLabsResponse is the single-channel batch transcription body.
type elevenLabsResponse struct {
	Text              string  `json:"text"`
	AudioDurationSecs float64 `json:"audio_duration_secs"`
}

// transcribe sends one batch request and returns the transcript and the audio
// seconds the response reports (0 when it reports none).
func (s *ElevenLabsService) transcribe(
	ctx context.Context, audio []byte, nc *normalizedConfig, config *TranscriptionConfig,
) (text string, seconds float64, err error) {
	fields := []formField{{"model_id", nc.model}}
	var filename string
	if nc.format == FormatPCM && nc.sampleRate == elevenLabsRawPCMRate &&
		nc.channels == 1 && nc.bitDepth == DefaultBitDepth {
		filename = rawPCMFilename
		fields = append(fields, formField{"file_format", elevenLabsRawPCMFormat})
	} else {
		audio, filename = containerAudio(audio, nc)
	}
	if config.Language != "" {
		fields = append(fields, formField{"language_code", config.Language})
	}
	// A transcript feeds a model as the user's words; "(laughter)"-style
	// event tags are not speech.
	fields = append(fields, formField{"tag_audio_events", "false"})

	headers := map[string]string{}
	if s.APIKey != "" {
		headers["xi-api-key"] = s.APIKey
	}
	upload := &batchUpload{
		provider: providerElevenLabs,
		url:      s.BaseURL + elevenLabsTranscribeEndpoint,
		headers:  headers,
		fields:   fields,
		filename: filename,
		audio:    audio,
	}
	body, err := upload.send(ctx, s.Client, elevenLabsError)
	if err != nil {
		return "", 0, err
	}
	var resp elevenLabsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", 0, fmt.Errorf("failed to parse response: %w", err)
	}
	return resp.Text, resp.AudioDurationSecs, nil
}

// elevenLabsError maps an error response. Its detail is an object carrying
// status and message, or, for a 422, a list of validation errors.
func elevenLabsError(status int, body []byte) error {
	var envelope struct {
		Detail json.RawMessage `json:"detail"`
	}
	var code, message string
	if json.Unmarshal(body, &envelope) == nil {
		var obj struct {
			Status  string `json:"status"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		var list []struct {
			Msg string `json:"msg"`
		}
		switch {
		case json.Unmarshal(envelope.Detail, &obj) == nil:
			code, message = obj.Status, obj.Message
			if code == "" {
				code = obj.Code
			}
		case json.Unmarshal(envelope.Detail, &list) == nil && len(list) > 0:
			message = list[0].Msg
		}
	}
	return statusError(providerElevenLabs, status, code, message, body)
}

// SupportedFormats returns the audio formats the service sends.
func (s *ElevenLabsService) SupportedFormats() []string {
	return batchSupportedFormats()
}
