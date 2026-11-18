package main

// showcase.go holds everything both modes share: the providers, the
// show_picture tool and the event log. It builds without PortAudio, so CI
// compiles and tests it; main_interactive.go (mic and speaker) needs the
// portaudio build tag, and main_text.go is the scripted text mode.

import (
	"bytes"
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

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
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
)

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
	// tts speaks a guardrail's message (-tts); nil plays a short tone.
	tts tts.Service
}

// setupFor returns the realtime provider named by the -provider flag. The
// default models are the ones the repository's live tests run against.
func setupFor(name string) (*providerSetup, error) {
	switch name {
	case "openai":
		return &providerSetup{
			name: "OpenAI Realtime", envKey: "OPENAI_API_KEY", model: "gpt-realtime",
			inputRate: 24000, encoding: "pcm16", judgeType: "openai", judgeModel: "gpt-4.1-mini",
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

// flags are the command-line options both modes take.
type flags struct {
	provider string
	model    string
	tts      string
}

// parseFlags reads the command line and resolves the provider, checking its
// API key is set.
func parseFlags() (*providerSetup, error) {
	var f flags
	flag.StringVar(&f.provider, "provider", "openai", "realtime provider: openai or gemini")
	flag.StringVar(&f.model, "model", "", "model (default: gpt-realtime / gemini-3.8-live)")
	flag.StringVar(&f.tts, "tts", "", "TTS service that speaks guardrail messages: openai or gemini (default: none, a tone)")
	flag.Parse()
	setup, err := setupFor(f.provider)
	if err != nil {
		return nil, err
	}
	if f.model != "" {
		setup.model = f.model
	}
	if os.Getenv(setup.envKey) == "" {
		return nil, fmt.Errorf("%s is not set", setup.envKey)
	}
	if err := setup.useTTS(f.tts); err != nil {
		return nil, err
	}
	return setup, nil
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
// caller hears it as. With a TTS service the message is spoken from it, in
// the session's voice when the service has a voice of that name; without one
// the caller hears a short tone and sees the text.
func (p *providerSetup) cueOptions() (opts []sdk.Option, heard string) {
	if p.tts == nil {
		return nil, "a short tone (no TTS service; add -tts openai to hear the words)"
	}
	return []sdk.Option{sdk.WithTTS(p.tts)}, "spoken by " + p.tts.Name() + " TTS"
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

// sessionOptions are the options both modes open the conversation with: the
// event bus the log watches and the store it reads replies from.
func sessionOptions(bus events.Bus, store statestore.Store) []sdk.Option {
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
