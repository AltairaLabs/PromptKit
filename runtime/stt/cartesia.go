package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Compile-time check: CartesiaService satisfies Service.
var _ Service = (*CartesiaService)(nil)

const (
	cartesiaBaseURL            = "https://api.cartesia.ai"
	cartesiaTranscribeEndpoint = "/stt"
	providerCartesia           = "cartesia"

	// cartesiaAPIVersion is the Cartesia-Version header every request carries.
	cartesiaAPIVersion = "2026-08-14"

	// CartesiaModelInkWhisper is the Cartesia Ink batch transcription model,
	// the one CartesiaService uses by default.
	CartesiaModelInkWhisper = "ink-whisper"

	defaultCartesiaTimeout = 60 * time.Second

	// cartesiaPCMEncoding is the encoding query value for 16-bit
	// little-endian PCM, which the endpoint takes without a container.
	cartesiaPCMEncoding = "pcm_s16le"

	// cartesiaPerSecondRate is the batch ink-whisper rate at Cartesia's Scale
	// plan: 1 credit per 2 seconds at $299 for 8M credits, about $0.07 an hour.
	cartesiaPerSecondRate = 0.5 * 299.0 / 8_000_000
)

// cartesiaDefaultPricing is the inline fallback price for batch ink-whisper.
var cartesiaDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: currencyUSD,
	Items:    []base.PriceItem{{Unit: unitSecond, Rate: cartesiaPerSecondRate}},
}

// CartesiaService implements STT with the Cartesia Ink batch API.
type CartesiaService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
}

// CartesiaOption configures the Cartesia STT service. It is a type alias for
// base.HTTPServiceOption, so base.WithBaseURL, base.WithClient and
// base.WithModel apply directly.
type CartesiaOption = base.HTTPServiceOption

// NewCartesia creates a Cartesia STT service. The model defaults to
// CartesiaModelInkWhisper.
func NewCartesia(apiKey string, opts ...CartesiaOption) *CartesiaService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    providerCartesia,
		Type:    base.ProviderTypeSTT,
		Pricing: cartesiaDefaultPricing,
		BaseURL: cartesiaBaseURL,
		Model:   CartesiaModelInkWhisper,
		Timeout: defaultCartesiaTimeout,
	}, opts...)
	return &CartesiaService{Implementation: impl, HTTPServiceFields: fields}
}

// Transcribe implements base.STTProvider. The cost prices the audio duration
// the response reports, or, when it reports none, the length of PCM/WAV input.
func (s *CartesiaService) Transcribe(ctx context.Context, req base.STTRequest) (base.STTResponse, error) {
	return transcribeWithCost(ctx, s.Implementation, s.Model, req, s.transcribe)
}

// TranscribeBytes converts audio to text and returns the transcript only, with
// no cost. config.Prompt is not sent.
func (s *CartesiaService) TranscribeBytes(
	ctx context.Context, audio []byte, config TranscriptionConfig,
) (string, error) {
	return transcribeBytes(ctx, audio, config, s.Model, s.transcribe)
}

// cartesiaResponse is the batch transcription body.
type cartesiaResponse struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration"`
}

// transcribe sends one batch request and returns the transcript and the audio
// seconds the response reports (0 when it reports none). 16-bit PCM goes up
// raw, its encoding and sample rate in the query; other PCM is wrapped as WAV.
func (s *CartesiaService) transcribe(
	ctx context.Context, audio []byte, nc *normalizedConfig, config *TranscriptionConfig,
) (text string, seconds float64, err error) {
	endpoint := s.BaseURL + cartesiaTranscribeEndpoint
	var filename string
	if nc.format == FormatPCM && nc.channels == 1 && nc.bitDepth == DefaultBitDepth {
		filename = rawPCMFilename
		q := url.Values{}
		q.Set("encoding", cartesiaPCMEncoding)
		q.Set("sample_rate", strconv.Itoa(nc.sampleRate))
		endpoint += "?" + q.Encode()
	} else {
		audio, filename = containerAudio(audio, nc)
	}
	fields := []formField{{"model", nc.model}}
	if config.Language != "" {
		fields = append(fields, formField{"language", config.Language})
	}

	headers := map[string]string{"Cartesia-Version": cartesiaAPIVersion}
	if s.APIKey != "" {
		headers["Authorization"] = "Bearer " + s.APIKey
	}
	upload := &batchUpload{
		provider: providerCartesia,
		url:      endpoint,
		headers:  headers,
		fields:   fields,
		filename: filename,
		audio:    audio,
	}
	body, err := upload.send(ctx, s.Client, cartesiaError)
	if err != nil {
		return "", 0, err
	}
	var resp cartesiaResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", 0, fmt.Errorf("failed to parse response: %w", err)
	}
	return resp.Text, resp.Duration, nil
}

// cartesiaError maps an error response, whose body carries error and message.
func cartesiaError(status int, body []byte) error {
	var resp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &resp) // an undecodable body falls back to the raw text
	message := resp.Message
	if message == "" {
		message = resp.Error
	}
	return statusError(providerCartesia, status, "", message, body)
}

// SupportedFormats returns the audio formats the service sends.
func (s *CartesiaService) SupportedFormats() []string {
	return batchSupportedFormats()
}
