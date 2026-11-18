package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

var _ base.TTSProvider = (*GeminiService)(nil)

const (
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

	// DefaultGeminiTTSModel is the Gemini TTS model a service uses when its
	// configuration names none (a provider spec's model, base.WithModel, or
	// SynthesisConfig.Model).
	DefaultGeminiTTSModel = "gemini-3.8-flash-tts"

	// GeminiDefaultVoice is the prebuilt voice spoken when SynthesisConfig.Voice
	// is empty: Gemini Live's default voice, so a cue and an unconfigured Live
	// session sound alike.
	GeminiDefaultVoice = "Puck"

	// defaultGeminiTimeout bounds a synthesis request. The whole utterance
	// comes back in one response, so it is longer than the streaming services'.
	defaultGeminiTimeout = 60 * time.Second

	// geminiSampleRate is the rate Gemini TTS returns PCM at when the
	// response does not name one.
	geminiSampleRate = 24000

	geminiAPIKeyHeader = "x-goog-api-key" //nolint:gosec // G101: a header name, not a credential
	geminiModalAudio   = "AUDIO"
)

// GeminiService implements TTS with Gemini's speech generation: a
// generateContent call with AUDIO output, on the Gemini API (API key) or on
// Vertex AI (platform credential). It returns 16-bit mono PCM.
type GeminiService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client

	// vertex selects the Vertex AI URL shape ({base}/{model}:generateContent),
	// where BaseURL already ends in the publisher's models collection.
	vertex bool
}

// GeminiOption configures the Gemini TTS service. It is base.HTTPServiceOption,
// so base.WithBaseURL, base.WithClient and base.WithModel apply.
type GeminiOption = base.HTTPServiceOption

// NewGemini creates a Gemini TTS service on the Gemini API, authenticated
// with apiKey (sent in the x-goog-api-key header).
func NewGemini(apiKey string, opts ...GeminiOption) *GeminiService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    providerGemini,
		Type:    base.ProviderTypeTTS,
		BaseURL: geminiBaseURL,
		Model:   DefaultGeminiTTSModel,
		Timeout: defaultGeminiTimeout,
	}, opts...)
	return &GeminiService{Implementation: impl, HTTPServiceFields: fields}
}

// NewGeminiOnVertex creates a Gemini TTS service on Vertex AI in a project and
// location. Every request carries cred (a GCP OAuth token) and no API key.
// base.WithBaseURL replaces the location's Google publisher models URL.
func NewGeminiOnVertex(
	cred credentials.Credential, project, location string, opts ...GeminiOption,
) *GeminiService {
	client := &http.Client{
		Timeout:   defaultGeminiTimeout,
		Transport: &base.CredentialTransport{Base: http.DefaultTransport, Cred: cred},
	}
	vertexOpts := []GeminiOption{
		base.WithClient(client),
		base.WithBaseURL(credentials.VertexEmbeddingEndpoint(project, location)),
	}
	svc := NewGemini("", append(vertexOpts, opts...)...)
	svc.vertex = true
	return svc
}

// ImplName returns the implementation name for cost tracking.
func (s *GeminiService) ImplName() string { return providerGemini }

// ModelName returns the configured model name for cost tracking.
func (s *GeminiService) ModelName() string { return s.Model }

// geminiTTSRequest is the generateContent body for speech generation.
type geminiTTSRequest struct {
	Contents         []geminiTTSContent `json:"contents"`
	GenerationConfig geminiTTSGenConfig `json:"generationConfig"`
}

type geminiTTSContent struct {
	Parts []geminiTTSPart `json:"parts"`
}

type geminiTTSPart struct {
	Text       string           `json:"text,omitempty"`
	InlineData *geminiTTSInline `json:"inlineData,omitempty"`
}

type geminiTTSInline struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiTTSGenConfig struct {
	ResponseModalities []string              `json:"responseModalities"`
	SpeechConfig       geminiTTSSpeechConfig `json:"speechConfig"`
}

type geminiTTSSpeechConfig struct {
	LanguageCode string               `json:"languageCode,omitempty"`
	VoiceConfig  geminiTTSVoiceConfig `json:"voiceConfig"`
}

type geminiTTSVoiceConfig struct {
	PrebuiltVoiceConfig struct {
		VoiceName string `json:"voiceName"`
	} `json:"prebuiltVoiceConfig"`
}

// geminiTTSResponse is the part of a generateContent response that carries audio.
type geminiTTSResponse struct {
	Candidates []struct {
		Content      geminiTTSContent `json:"content"`
		FinishReason string           `json:"finishReason"`
	} `json:"candidates"`
}

// Synthesize converts text to 16-bit mono PCM. The reply's audio (a WAV or
// raw PCM, depending on the model) is unwrapped to its samples and resampled
// to config.Format.SampleRate when that is set and differs from the reply's.
func (s *GeminiService) Synthesize(ctx context.Context, text string, config SynthesisConfig) (io.ReadCloser, error) {
	if text == "" {
		return nil, ErrEmptyText
	}
	if config.Format.Name != "" && config.Format.Name != formatPCM {
		return nil, NewSynthesisError(providerGemini, "",
			fmt.Sprintf("Gemini TTS returns pcm audio, not %q", config.Format.Name), ErrInvalidFormat, false)
	}
	model := config.Model
	if model == "" {
		model = s.Model
	}
	voice := config.Voice
	if voice == "" {
		voice = GeminiDefaultVoice
	}

	body := geminiTTSRequest{
		Contents: []geminiTTSContent{{Parts: []geminiTTSPart{{Text: text}}}},
		GenerationConfig: geminiTTSGenConfig{
			ResponseModalities: []string{geminiModalAudio},
			SpeechConfig:       geminiTTSSpeechConfig{LanguageCode: config.Language},
		},
	}
	body.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName = voice

	headers := map[string]string{headerContentType: mimeJSON}
	if s.APIKey != "" {
		headers[geminiAPIKeyHeader] = s.APIKey
	}
	rc, err := postJSONForAudio(ctx, s.Client, providerGemini, s.generateContentURL(model), body, headers,
		s.handleError)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var resp geminiTTSResponse
	if decodeErr := json.NewDecoder(rc).Decode(&resp); decodeErr != nil {
		return nil, NewSynthesisError(providerGemini, "", "decoding the response", decodeErr, true)
	}
	pcm, rate, err := geminiResponsePCM(&resp)
	if err != nil {
		return nil, err
	}
	if want := config.Format.SampleRate; want > 0 && want != rate {
		if pcm, err = audio.ResamplePCM16(pcm, rate, want); err != nil {
			return nil, NewSynthesisError(providerGemini, "", "resampling the audio", err, false)
		}
	}
	return io.NopCloser(bytes.NewReader(pcm)), nil
}

// generateContentURL is {base}/models/{model}:generateContent on the Gemini
// API and {base}/{model}:generateContent on Vertex, whose base URL already
// ends in the publisher's models collection.
func (s *GeminiService) generateContentURL(model string) string {
	if s.vertex {
		return s.BaseURL + "/" + model + ":generateContent"
	}
	return s.BaseURL + "/models/" + model + ":generateContent"
}

// geminiResponsePCM returns the first inline audio part's samples and rate.
func geminiResponsePCM(resp *geminiTTSResponse) (pcm []byte, rate int, err error) {
	finish := ""
	for _, c := range resp.Candidates {
		finish = c.FinishReason
		for _, p := range c.Content.Parts {
			if p.InlineData == nil {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(p.InlineData.Data)
			if err != nil {
				return nil, 0, NewSynthesisError(providerGemini, "", "decoding the audio", err, false)
			}
			return geminiAudioPCM(p.InlineData.MimeType, raw)
		}
	}
	return nil, 0, NewSynthesisError(providerGemini, "",
		fmt.Sprintf("no audio in the response (finish reason %s)", finish), ErrSynthesisFailed, false)
}

// geminiAudioPCM unwraps an inline audio part: a WAV is reduced to its data
// chunk (Gemini appends a C2PA manifest chunk after it), and raw PCM
// (audio/L16, audio/pcm) is taken as is at its rate parameter.
func geminiAudioPCM(mimeType string, raw []byte) (pcm []byte, rate int, err error) {
	mediaType, params, _ := mime.ParseMediaType(mimeType)
	switch strings.ToLower(mediaType) {
	case FormatWAV.MIMEType, "audio/x-wav", "audio/wave":
		samples, wavRate, why := wavMonoPCM16(raw)
		if why != "" {
			return nil, 0, NewSynthesisError(providerGemini, "", why, ErrInvalidFormat, false)
		}
		return samples, wavRate, nil
	case "audio/l16", FormatPCM16.MIMEType:
		rate = geminiSampleRate
		if r, convErr := strconv.Atoi(params["rate"]); convErr == nil && r > 0 {
			rate = r
		}
		return raw, rate, nil
	default:
		return nil, 0, NewSynthesisError(providerGemini, "",
			fmt.Sprintf("unsupported audio type %q", mimeType), ErrInvalidFormat, false)
	}
}

// WAV layout: a 12-byte RIFF header, then chunks of a 4-byte ID, a 4-byte
// little-endian size and the body, padded to an even length.
const (
	wavRIFFHeaderLen  = 12
	wavChunkHeaderLen = 8
	wavFmtMinLen      = 16
	wavFormatPCM      = 1
	wavChunkAlign     = 2 // a chunk body of odd length is followed by a pad byte
)

// wavMonoPCM16 returns a mono 16-bit PCM WAV's samples and rate, or says why
// it cannot.
func wavMonoPCM16(raw []byte) (pcm []byte, rate int, why string) {
	if len(raw) < wavRIFFHeaderLen || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, "audio is not a WAV"
	}
	var fmtBody []byte
	haveData := false
	for i := wavRIFFHeaderLen; i+wavChunkHeaderLen <= len(raw); {
		id := string(raw[i : i+4])
		size := int(binary.LittleEndian.Uint32(raw[i+4:]))
		start := i + wavChunkHeaderLen
		end := start + size
		if end > len(raw) || end < start {
			end = len(raw) // a streamed WAV's data size can be unknown
		}
		switch id {
		case "fmt ":
			fmtBody = raw[start:end]
		case "data":
			pcm, haveData = raw[start:end], true
		}
		i = end + (end-start)%wavChunkAlign
	}
	if len(fmtBody) < wavFmtMinLen || !haveData {
		return nil, 0, "WAV has no fmt and data chunks"
	}
	format := binary.LittleEndian.Uint16(fmtBody)
	channels := binary.LittleEndian.Uint16(fmtBody[2:])
	bits := binary.LittleEndian.Uint16(fmtBody[14:])
	if format != wavFormatPCM || channels != 1 || bits != bitDepthDefault {
		return nil, 0, "WAV is not mono 16-bit PCM"
	}
	return pcm, int(binary.LittleEndian.Uint32(fmtBody[4:])), ""
}

// geminiErrorResponse is a Google API error body.
type geminiErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// handleError turns a non-200 response into a SynthesisError.
func (s *GeminiService) handleError(resp *http.Response) error {
	var errResp geminiErrorResponse
	if err := decodeErrorBody(providerGemini, resp, &errResp); err != nil {
		return err
	}
	retryable, cause := classifyHTTPStatus(resp.StatusCode, nil, nil)
	return NewSynthesisError(providerGemini, errResp.Error.Status, errResp.Error.Message, cause, retryable)
}

// geminiVoices are Gemini's prebuilt voices, the set Gemini Live speaks with
// too, with the style Google gives each. Gemini TTS detects the language from
// the text, so no voice is tied to one.
var geminiVoices = [][2]string{
	{"Zephyr", styleBright}, {"Puck", styleUpbeat}, {"Charon", styleInformative}, {"Kore", styleFirm},
	{"Fenrir", "Excitable"}, {"Leda", "Youthful"}, {"Orus", styleFirm}, {"Aoede", "Breezy"},
	{"Callirrhoe", styleEasyGoing}, {"Autonoe", styleBright}, {"Enceladus", "Breathy"}, {"Iapetus", styleClear},
	{"Umbriel", styleEasyGoing}, {"Algieba", styleSmooth}, {"Despina", styleSmooth}, {"Erinome", styleClear},
	{"Algenib", "Gravelly"}, {"Rasalgethi", styleInformative}, {"Laomedeia", styleUpbeat}, {"Achernar", "Soft"},
	{"Alnilam", styleFirm}, {"Schedar", "Even"}, {"Gacrux", "Mature"}, {"Pulcherrima", "Forward"},
	{"Achird", "Friendly"}, {"Zubenelgenubi", "Casual"}, {"Vindemiatrix", "Gentle"}, {"Sadachbia", "Lively"},
	{"Sadaltager", "Knowledgeable"}, {"Sulafat", "Warm"},
}

// Voice styles more than one Gemini voice shares.
const (
	styleBright      = "Bright"
	styleUpbeat      = "Upbeat"
	styleInformative = "Informative"
	styleFirm        = "Firm"
	styleEasyGoing   = "Easy-going"
	styleClear       = "Clear"
	styleSmooth      = "Smooth"
)

// SupportedVoices returns Gemini's prebuilt voices. The ID is the name a
// request (and a Gemini Live session's metadata["voice"]) uses.
func (s *GeminiService) SupportedVoices() []Voice {
	voices := make([]Voice, len(geminiVoices))
	for i, v := range geminiVoices {
		voices[i] = makeVoice(v[0], v[0], "", "", v[1])
	}
	return voices
}

// SupportedFormats returns the one format Gemini TTS produces: 24 kHz 16-bit
// mono PCM (Synthesize resamples it to Format.SampleRate on request).
func (s *GeminiService) SupportedFormats() []AudioFormat {
	return []AudioFormat{FormatPCM16}
}
