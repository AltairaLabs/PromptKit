package main

// showcase.go holds everything the voice and scripted modes share: the
// providers, the session modes (ASM and VAD), the show_picture tool and the
// event log. It builds without PortAudio, so CI compiles and tests it;
// main_interactive.go (mic and speaker) needs the portaudio build tag, and
// main_text.go is the scripted mode.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const (
	packPath   = "./realtime-showcase.pack.json"
	promptName = "showcase"

	// pictureNumber is drawn in the picture show_picture returns. The model
	// can only say it by reading the image.
	pictureNumber = "47"

	// outputSampleRate is the reply audio rate of both providers.
	outputSampleRate = 24000

	// judgeProviderKey is the logical provider the pack's no-bruno-topic
	// guardrail names in its requires block; judgeProvider binds it.
	judgeProviderKey = "topic-judge"

	// modeASM opens a realtime session on a native audio model (OpenAI
	// Realtime, Gemini Live); modeVAD runs voice activity detection, STT, a
	// text model and TTS in the pipeline.
	modeASM = "asm"
	modeVAD = "vad"

	// vadSampleRate is the rate VAD mode takes the caller's audio at: the
	// microphone captures at it and the scripted mode synthesizes its
	// requests at it. VAD mode does not resample its input.
	vadSampleRate = 16000
)

// speechKeys is the environment variable the API key of each speech service
// is read from, for -stt and -tts in VAD mode. A service type not listed here
// reads <TYPE>_API_KEY. The services themselves are whatever the runtime's
// STT and TTS factories have registered.
var speechKeys = map[string]string{
	"openai":     "OPENAI_API_KEY",
	"elevenlabs": "ELEVENLABS_API_KEY",
	"cartesia":   "CARTESIA_API_KEY",
	"gemini":     "GEMINI_API_KEY",
}

// providerSetup is one realtime provider: how to build it and the audio it
// takes.
type providerSetup struct {
	name      string
	envKey    string
	model     string
	inputRate int
	encoding  string
	metadata  map[string]interface{}
	build     func(model string) providers.Provider
	// judgeType and judgeModel are the text model that judges the caller's
	// words for the no-bruno-topic guardrail, on the same API key.
	judgeType  string
	judgeModel string
	// tts speaks a guardrail's message (-tts) when the session cannot say it
	// itself; nil plays a short tone.
	tts tts.Service

	// mode is modeASM or modeVAD (-mode).
	mode string
	// textName and textModel are the text model VAD mode talks to; -model
	// overrides textModel in VAD mode.
	textName  string
	textModel string
	// stt transcribes the caller in VAD mode; tts then speaks every reply,
	// in voice (-voice; "" is the service's own default).
	stt   stt.Service
	voice string
	// callerTTS speaks the scripted mode's requests in VAD mode: OpenAI TTS,
	// in a voice of its own.
	callerTTS tts.Service
}

// setupFor returns the realtime provider named by the -provider flag. The
// default models are the ones the repository's live tests run against.
func setupFor(name string) (*providerSetup, error) {
	switch name {
	case "openai":
		return &providerSetup{
			name: "OpenAI Realtime", envKey: "OPENAI_API_KEY", model: "gpt-realtime",
			inputRate: 24000, encoding: "pcm16", judgeType: "openai", judgeModel: "gpt-4.1-mini",
			textName: "OpenAI", textModel: "gpt-4.1-mini",
			metadata: map[string]interface{}{"voice": "alloy", "modalities": []string{"audio"}},
			build: func(model string) providers.Provider {
				return openai.NewProvider("openai-realtime", model, "https://api.openai.com",
					providers.ProviderDefaults{}, false)
			},
		}, nil
	case "gemini":
		return &providerSetup{
			name: "Gemini Live", envKey: "GEMINI_API_KEY", model: "gemini-3.8-live",
			inputRate: 16000, encoding: "pcm_linear16", judgeType: "gemini", judgeModel: "gemini-2.5-flash",
			textName: "Gemini", textModel: "gemini-2.5-flash",
			metadata: map[string]interface{}{"response_modalities": []string{"AUDIO"}},
			build: func(model string) providers.Provider {
				return gemini.NewProvider("gemini-live", model,
					"https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider %q (want openai or gemini)", name)
	}
}

// streamingConfig is the session's audio format: 16-bit mono PCM at the
// provider's input rate, sent in 100 ms chunks.
func (p *providerSetup) streamingConfig() *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			SampleRate: p.inputRate,
			Channels:   1,
			Encoding:   p.encoding,
			BitDepth:   16,
			ChunkSize:  p.inputRate / 10 * 2,
		},
		Metadata: p.metadata,
	}
}

// judgeProvider binds the pack's topic-judge to the provider's text model.
// The pack names only the logical key; which model judges is the host's
// choice.
func (p *providerSetup) judgeProvider() sdk.Option {
	return sdk.WithNamedProvider(sdk.ProviderSpec{ID: judgeProviderKey, Type: p.judgeType, Model: p.judgeModel})
}

// flags are the command-line options the voice and scripted modes take.
type flags struct {
	provider string
	mode     string
	model    string
	stt      string
	tts      string
	voice    string
}

// parseFlags reads the command line and resolves the provider and mode,
// checking the API keys they need are set.
func parseFlags() (*providerSetup, error) {
	var f flags
	flag.StringVar(&f.provider, "provider", "openai", "provider: openai or gemini")
	flag.StringVar(&f.mode, "mode", modeASM,
		"asm: the provider's realtime audio model; vad: VAD, an STT service, the provider's text model, a TTS service")
	flag.StringVar(&f.model, "model", "",
		"model (default: gpt-realtime / gemini-3.8-live; with -mode vad, gpt-4.1-mini / gemini-2.5-flash)")
	flag.StringVar(&f.tts, "tts", "",
		"-mode asm: TTS service that speaks guardrail messages: openai or gemini (default: none, a tone); "+
			"-mode vad: TTS service that speaks every reply: openai (default), elevenlabs, cartesia or gemini")
	flag.StringVar(&f.stt, "stt", "", "-mode vad: STT service that transcribes you: "+
		strings.Join(stt.RegisteredTypes(), ", ")+" (default: openai)")
	flag.StringVar(&f.voice, "voice", "", "-mode vad: the TTS voice (default: the service's own)")
	flag.Parse()
	return resolve(f)
}

// resolve turns the flags into a provider setup.
func resolve(f flags) (*providerSetup, error) {
	setup, err := setupFor(f.provider)
	if err != nil {
		return nil, err
	}
	if os.Getenv(setup.envKey) == "" {
		return nil, fmt.Errorf("%s is not set", setup.envKey)
	}
	switch f.mode {
	case modeASM:
		if f.voice != "" || f.stt != "" {
			return nil, errors.New("-stt and -voice apply to -mode vad")
		}
		setup.mode = modeASM
		if f.model != "" {
			setup.model = f.model
		}
		err = setup.useTTS(f.tts)
	case modeVAD:
		if f.model != "" {
			setup.textModel = f.model
		}
		err = setup.useVAD(f.stt, f.tts, f.voice)
	default:
		err = fmt.Errorf("unknown mode %q (want asm or vad)", f.mode)
	}
	if err != nil {
		return nil, err
	}
	return setup, nil
}

// useVAD switches the setup to VAD mode: the STT service sttName hears the
// caller, and the TTS service ttsName speaks every reply, including a
// guardrail's message, in voice ("" for the service's own default voice).
// Both default to openai.
func (p *providerSetup) useVAD(sttName, ttsName, voice string) error {
	sttName = cmp.Or(sttName, "openai")
	ttsName = cmp.Or(ttsName, "openai")
	if !slices.Contains(stt.RegisteredTypes(), sttName) {
		return fmt.Errorf("unknown STT service %q (want %s)", sttName, strings.Join(stt.RegisteredTypes(), ", "))
	}
	if !slices.Contains(tts.RegisteredTypes(), ttsName) {
		return fmt.Errorf("unknown TTS service %q (want %s)", ttsName, strings.Join(tts.RegisteredTypes(), ", "))
	}
	sttCred, err := speechCredential("stt", sttName)
	if err != nil {
		return err
	}
	ttsCred, err := speechCredential("tts", ttsName)
	if err != nil {
		return err
	}
	if p.stt, err = stt.CreateFromSpec(stt.ProviderSpec{ID: "showcase-stt", Type: sttName, Credential: sttCred}); err != nil {
		return fmt.Errorf("-stt %s: %w", sttName, err)
	}
	if p.tts, err = tts.CreateFromSpec(tts.ProviderSpec{ID: "showcase-tts", Type: ttsName, Credential: ttsCred}); err != nil {
		return fmt.Errorf("-tts %s: %w", ttsName, err)
	}
	p.mode = modeVAD
	p.voice = voice
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		p.callerTTS = tts.NewOpenAI(key)
	}
	return nil
}

// speechCredential resolves the API key of the speech service name, given
// with -flagName, from its environment variable (speechKeys).
func speechCredential(flagName, name string) (credentials.Credential, error) {
	env, ok := speechKeys[name]
	if !ok {
		env = strings.ToUpper(name) + "_API_KEY"
	}
	if os.Getenv(env) == "" {
		return nil, fmt.Errorf("-%s %s needs %s", flagName, name, env)
	}
	cred, err := base.ResolveCredential(context.Background(), name, "", &credentials.CredentialConfig{CredentialEnv: env})
	if err != nil {
		return nil, fmt.Errorf("-%s %s: %w", flagName, name, err)
	}
	return cred, nil
}

// banner is the first line the showcase prints: the mode, the model, and what
// plays a guardrail's message.
func (p *providerSetup) banner() string {
	_, heard := p.sessionOptions()
	if p.mode == modeVAD {
		voice := "its default voice"
		if p.voice != "" {
			voice = "voice " + p.voice
		}
		return fmt.Sprintf("Realtime showcase - VAD mode: %s %s, %s STT, %s TTS (%s); guardrail messages: %s",
			p.textName, p.textModel, p.stt.Name(), p.tts.Name(), voice, heard)
	}
	return fmt.Sprintf("Realtime showcase - ASM mode: %s (%s); guardrail messages: %s", p.name, p.model, heard)
}

// sessionOptions are the options that open the mode's session, and what a
// guardrail's message is heard as.
//
// ASM mode streams audio to the provider's realtime model
// (WithStreamingConfig). VAD mode runs the pipeline Audio -> VAD -> STT -> LLM
// -> TTS (WithVADMode) on the provider's text model: a turn ends when the
// caller falls silent, input guardrails judge the transcript before the model
// sees it, and the TTS service speaks a reply only once it is complete and has
// passed the output guardrails. A guardrail's message is the blocked turn's
// reply, so the same TTS service speaks it.
func (p *providerSetup) sessionOptions() (opts []sdk.Option, heard string) {
	if p.mode == modeVAD {
		return []sdk.Option{
			sdk.WithLLMProvider(sdk.ProviderSpec{ID: "showcase-llm", Type: p.judgeType, Model: p.textModel}),
			sdk.WithVADMode(p.stt, p.tts, p.vadConfig()),
		}, "spoken by " + p.tts.Name() + " TTS, as every reply is"
	}
	opts, heard = p.cueOptions()
	return append([]sdk.Option{
		sdk.WithProvider(p.build(p.model)),
		sdk.WithStreamingConfig(p.streamingConfig()),
	}, opts...), heard
}

// vadConfig is VAD mode's turn detection and voice: the defaults, at
// vadSampleRate, in the -voice voice ("" leaves the TTS service's own).
func (p *providerSetup) vadConfig() *sdk.VADModeConfig {
	cfg := sdk.DefaultVADModeConfig()
	cfg.SampleRate = vadSampleRate
	cfg.Voice = p.voice
	return cfg
}

// captureRate is the rate the microphone records at: the realtime model's
// input rate in ASM mode, vadSampleRate in VAD mode.
func (p *providerSetup) captureRate() int {
	if p.mode == modeVAD {
		return vadSampleRate
	}
	return p.inputRate
}

// useTTS sets the TTS service that speaks a guardrail's message: "openai"
// for OpenAI TTS, "gemini" for Gemini TTS, "" for none.
func (p *providerSetup) useTTS(name string) error {
	switch name {
	case "":
		return nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return errors.New("-tts openai needs OPENAI_API_KEY")
		}
		p.tts = tts.NewOpenAI(key)
		return nil
	case "gemini":
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return errors.New("-tts gemini needs GEMINI_API_KEY")
		}
		p.tts = tts.NewGemini(key)
		return nil
	default:
		return fmt.Errorf("unknown TTS service %q (want openai or gemini)", name)
	}
}

// cueOptions are the options that voice a guardrail's message, and what the
// caller hears it as. A session that can speak outside the conversation
// (OpenAI Realtime) says the message itself, in its own voice; the library
// finds that out from the session, and the event log reports which source
// rendered each message. Otherwise the TTS service (-tts) speaks it, in the
// session's voice when the service has a voice of that name; without one the
// caller hears a short tone and sees the text.
func (p *providerSetup) cueOptions() (opts []sdk.Option, heard string) {
	if p.tts == nil {
		return nil, "the session's own voice when it can speak out of band, else a short tone " +
			"(add -tts openai to hear the words)"
	}
	return []sdk.Option{sdk.WithCueVoice(p.tts)},
		"the session's own voice when it can speak out of band, else " + p.tts.Name() + " TTS"
}

// pictureExecutor runs show_picture: it returns only a PNG, so the model has
// to look at the image to answer.
type pictureExecutor struct{ png string }

// newPictureExecutor renders the picture once.
func newPictureExecutor() (*pictureExecutor, error) {
	data, err := renderNumberPNG(pictureNumber)
	if err != nil {
		return nil, err
	}
	return &pictureExecutor{png: base64.StdEncoding.EncodeToString(data)}, nil
}

// Name implements tools.Executor.
func (e *pictureExecutor) Name() string { return "show-picture" }

// Execute implements tools.Executor. The conversation calls ExecuteCall
// instead; only ExecuteCall can return the image.
func (e *pictureExecutor) Execute(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{"error":"show_picture returns an image"}`), nil
}

// ExecuteCall implements tools.CallExecutor: the result is the image alone.
func (e *pictureExecutor) ExecuteCall(
	context.Context, *tools.ToolDescriptor, tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) { //nolint:unparam // CallExecutor's signature: no JSON result
	return nil, []types.ContentPart{types.NewImagePartFromData(e.png, types.MIMETypeImagePNG, nil)}, nil
}

// digitGlyphs is a 5x7 bitmap of each digit.
var digitGlyphs = map[rune][7]string{
	'0': {".###.", "#...#", "#..##", "#.#.#", "##..#", "#...#", ".###."},
	'1': {"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", ".###."},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'3': {"#####", "...#.", "..#..", "...#.", "....#", "#...#", ".###."},
	'4': {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'5': {"#####", "#....", "####.", "....#", "....#", "#...#", ".###."},
	'6': {"..##.", ".#...", "#....", "####.", "#...#", "#...#", ".###."},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'8': {".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."},
	'9': {".###.", "#...#", "#...#", ".####", "....#", "...#.", ".##.."},
}

// renderNumberPNG draws digits large, black on white, and encodes the
// image as PNG.
func renderNumberPNG(digits string) ([]byte, error) {
	const scale, pad = 24, 24
	w := pad*2 + len(digits)*6*scale
	h := pad*2 + 7*scale
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for i, r := range digits {
		glyph, ok := digitGlyphs[r]
		if !ok {
			return nil, fmt.Errorf("no glyph for %q", r)
		}
		for row, line := range glyph {
			for col, c := range line {
				if c == '#' {
					fillSquare(img, pad+(i*6+col)*scale, pad+row*scale, scale)
				}
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// fillSquare paints a size x size black square at (x0, y0).
func fillSquare(img *image.Gray, x0, y0, size int) {
	for y := y0; y < y0+size; y++ {
		for x := x0; x < x0+size; x++ {
			img.SetGray(x, y, color.Gray{Y: 0})
		}
	}
}

// conversationID names the conversation in the state store the event log
// reads replies from.
const conversationID = "realtime-showcase"

// logOptions are the options every session opens the conversation with: the
// event bus the log watches and the store it reads replies from.
func logOptions(bus events.Bus, store statestore.Store) []sdk.Option {
	return []sdk.Option{
		sdk.WithEventBus(bus),
		sdk.WithStateStore(store),
		sdk.WithConversationID(conversationID),
	}
}

// registerTools binds show_picture to its executor.
func registerTools(conv *sdk.Conversation) error {
	exec, err := newPictureExecutor()
	if err != nil {
		return err
	}
	conv.OnToolExecutor("show_picture", exec)
	return nil
}
