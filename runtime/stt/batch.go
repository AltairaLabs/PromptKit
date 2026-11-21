package stt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// serverErrorThreshold is the HTTP status from which a response is a server
// error, and so retryable.
const serverErrorThreshold = 500

const (
	// secondsPerHour converts a per-hour rate to a per-second one.
	secondsPerHour = 3600
	// currencyUSD is the currency of the inline price tables.
	currencyUSD = "usd"
	// rawPCMFilename names a raw PCM upload.
	rawPCMFilename = "audio.pcm"
	// Compressed containers every service here uploads as they are.
	formatFLAC = "flac"
	formatM4A  = "m4a"
	formatOGG  = "ogg"
	formatWebM = "webm"
)

// batchSupportedFormats is the audio the ElevenLabs and Cartesia services
// send: PCM (raw or wrapped as WAV) and the containers both endpoints decode.
func batchSupportedFormats() []string {
	return []string{FormatPCM, FormatWAV, FormatMP3, formatFLAC, formatM4A, formatOGG, formatWebM}
}

// formField is one text field of a multipart transcription request.
type formField struct {
	name, value string
}

// batchUpload is one multipart POST to a vendor's batch transcription
// endpoint: the audio as the "file" part, then the text fields in order.
type batchUpload struct {
	provider string
	url      string
	headers  map[string]string
	fields   []formField
	filename string
	audio    []byte
}

// send posts the upload and returns the response body. A transport failure is
// a retryable TranscriptionError; a non-200 response is whatever decodeError
// makes of its status and body.
func (u *batchUpload) send(
	ctx context.Context, client *http.Client, decodeError func(status int, body []byte) error,
) ([]byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", u.filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err = part.Write(u.audio); err != nil {
		return nil, fmt.Errorf("failed to write audio data: %w", err)
	}
	for _, f := range u.fields {
		if err = w.WriteField(f.name, f.value); err != nil {
			return nil, fmt.Errorf("failed to write %s field: %w", f.name, err)
		}
	}
	if err = w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.url, &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range u.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, NewTranscriptionError(u.provider, "", "request failed", err, true)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, withRetryAfter(decodeError(resp.StatusCode, body), resp)
	}
	return body, nil
}

// withRetryAfter records the Retry-After of resp on err when err is a
// *TranscriptionError, so TranscribeWithRetry waits what the provider asked for.
func withRetryAfter(err error, resp *http.Response) error {
	var te *TranscriptionError
	if errors.As(err, &te) {
		te.RetryAfter = retry.ParseRetryAfter(resp.Header)
	}
	return err
}

// statusError builds the TranscriptionError for a non-200 response: 429 and
// 5xx are retryable, 429 is ErrRateLimited and 401 an invalid key. An empty
// code falls back to the status number, an empty message to the raw body.
func statusError(provider string, status int, code, message string, body []byte) error {
	if code == "" {
		code = strconv.Itoa(status)
	}
	if message == "" {
		message = string(body)
	}
	var cause error
	switch status {
	case http.StatusTooManyRequests:
		cause = ErrRateLimited
	case http.StatusUnauthorized:
		cause = fmt.Errorf("invalid API key")
	}
	retryable := status == http.StatusTooManyRequests || status >= serverErrorThreshold
	return NewTranscriptionError(provider, code, message, cause, retryable)
}

// transcribeWithCost runs one transcription for base.STTProvider: it applies
// the request's hints, calls transcribe, and prices the seconds it reports —
// or, when it reports none, the length of PCM/WAV input.
func transcribeWithCost(
	ctx context.Context,
	impl *base.Implementation,
	defaultModel string,
	req base.STTRequest,
	transcribe func(context.Context, []byte, *normalizedConfig, *TranscriptionConfig) (string, float64, error),
) (base.STTResponse, error) {
	if len(req.Audio) == 0 {
		return base.STTResponse{}, ErrEmptyAudio
	}
	start := time.Now()
	cfg := transcriptionConfigFromRequest(req)
	nc := normalizeTranscriptionConfig(&cfg, defaultModel)

	text, seconds, err := transcribe(ctx, req.Audio, nc, &cfg)
	if err != nil {
		return base.STTResponse{}, err
	}
	if seconds <= 0 {
		seconds = estimateAudioSeconds(req.Audio, req.MIMEType, nc)
	}

	latency := time.Since(start)
	resp := base.STTResponse{Text: text, Latency: latency}
	if pricing := impl.Pricing(); pricing != nil && seconds > 0 {
		resp.Cost = base.MakeCostInfo(pricing, impl.Name(), base.ProviderTypeSTT,
			map[string]float64{unitSecond: seconds}, latency)
	}
	return resp, nil
}

// pricedService is a Service whose price an additional_config pricing block
// can override.
type pricedService interface {
	Service
	SetPricing(*base.PricingDescriptor)
}

// registerAPIKeyFactory registers a factory for a service that authenticates
// with its own API key: spec.Model and spec.BaseURL override its defaults and
// additional_config pricing its price.
func registerAPIKeyFactory(
	providerType string, newService func(apiKey string, opts ...base.HTTPServiceOption) pricedService,
) {
	RegisterFactory(providerType, func(spec ProviderSpec) (Service, error) {
		opts := []base.HTTPServiceOption{}
		if spec.Model != "" {
			opts = append(opts, base.WithModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, base.WithBaseURL(spec.BaseURL))
		}
		svc := newService(APIKeyFromCredential(spec.Credential), opts...)
		if p := PricingFromSpec(spec); p != nil {
			svc.SetPricing(p)
		}
		return svc, nil
	})
}

// transcribeBytes runs one transcription for Service.TranscribeBytes and
// returns the transcript only.
func transcribeBytes(
	ctx context.Context, audio []byte, config TranscriptionConfig, defaultModel string,
	transcribe func(context.Context, []byte, *normalizedConfig, *TranscriptionConfig) (string, float64, error),
) (string, error) {
	if len(audio) == 0 {
		return "", ErrEmptyAudio
	}
	text, _, err := transcribe(ctx, audio, normalizeTranscriptionConfig(&config, defaultModel), &config)
	return text, err
}
