package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// The showcase without audio hardware: the example's pack, tool and event log
// on a scripted mock realtime session that plays the parts a live provider
// would — the caller's transcripts, reply text and audio, a barge-in, a tool
// call, and cost.

// scriptedProvider hands the conversation one scripted realtime session.
type scriptedProvider struct {
	*mock.StreamingProvider
	sess *mock.MockStreamSession
}

func (p *scriptedProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.sess, nil
}

// syncBuffer is an io.Writer the log and the test share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Split(strings.TrimRight(s.b.String(), "\n"), "\n")
}

// heardRe matches the wall-clock-dependent numbers of a barge-in line: how
// much of the reply played before the caller spoke.
var heardRe = regexp.MustCompile(`heard_ms=\d+ (audio_ms=\d+) played_fraction=[0-9.]+`)

// showcaseRun is a scripted session and the log lines it printed.
type showcaseRun struct {
	t    *testing.T
	sess *mock.MockStreamSession
	out  *syncBuffer
	seen int
	// speechCosts masks the running total on a step's lines: STT, TTS and
	// provider costs print from their own goroutines, so the order they add
	// up in is not fixed. The session's end gives the total.
	speechCosts bool
}

// totalRe matches a line's running session total.
var totalRe = regexp.MustCompile(`session total \$[0-9.]+$`)

// step drives one exchange and returns the lines it printed, sorted: the
// bus, the Response() reader and the store poll print from their own
// goroutines, so the order within a step is not fixed.
func (r *showcaseRun) step(want int, chunks ...*providers.StreamChunk) []string {
	r.t.Helper()
	for _, c := range chunks {
		r.sess.EmitChunk(c)
	}
	return r.collect(want)
}

// collect waits for want new lines and returns them sorted, as step does.
func (r *showcaseRun) collect(want int) []string {
	r.t.Helper()
	var got []string
	require.Eventuallyf(r.t, func() bool {
		got = r.out.lines()[r.seen:]
		return len(got) >= want
	}, 5*time.Second, 5*time.Millisecond, "want %d lines, have %q", want, r.out.lines()[r.seen:])
	time.Sleep(100 * time.Millisecond) // nothing more may arrive
	got = r.out.lines()[r.seen:]
	r.seen += len(got)
	out := make([]string, len(got))
	for i, l := range got {
		l = heardRe.ReplaceAllString(l, "heard_ms=N $1 played_fraction=F")
		if r.speechCosts {
			l = totalRe.ReplaceAllString(l, "session total $$T")
		}
		out[i] = l
	}
	sort.Strings(out)
	return out
}

func heard(text string) *providers.StreamChunk {
	return &providers.StreamChunk{Metadata: map[string]interface{}{
		"type": "input_transcription", "transcription": text, "transcription_final": true}}
}

func said(text string, audioMs int) *providers.StreamChunk {
	return &providers.StreamChunk{Delta: text, Metadata: map[string]interface{}{"type": "output_transcription"},
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1,
			Data: make([]byte, audioMs*48)}}
}

func done(cost float64, in, out int) *providers.StreamChunk {
	stop := types.FinishReasonStop
	return &providers.StreamChunk{FinishReason: &stop,
		CostInfo: &types.CostInfo{InputTokens: in, OutputTokens: out, TotalCost: cost}}
}

// A note the pipeline adds to the model's conversation after a guardrail
// block is printed in full, as what the model was told, with the guardrails
// that blocked the turn: the note never names them to the model.
func TestShowcaseEventLog_PrintsTheModelsNote(t *testing.T) {
	out := &syncBuffer{}
	elog := newEventLog(out, statestore.NewMemoryStore(), conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }

	elog.onEvent(&events.Event{Data: &events.ContextNoteData{
		Text: "Your previous reply was withheld.", Validator: "no-bruno"}})
	elog.onEvent(&events.Event{Data: &events.ContextNoteData{Text: "A message from the user was blocked."}})

	assert.Equal(t, []string{
		`09:30:00.000  note      told the model, not the caller (no-bruno): "Your previous reply was withheld."`,
		`09:30:00.000  note      told the model, not the caller: "A message from the user was blocked."`,
	}, out.lines())
}

// Late usage is labeled with the call its CallID names, not the call that
// completed last. Usage that arrives before its call's provider.call.completed
// (the bus delivers each event type on its own goroutine) is printed after
// the call's line; usage naming no call is the session's.
func TestShowcaseEventLog_LateUsageNamesItsCall(t *testing.T) {
	out := &syncBuffer{}
	elog := newEventLog(out, statestore.NewMemoryStore(), conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }
	completed := func(id string, cost float64) {
		elog.onEvent(&events.Event{Data: &events.ProviderCallCompletedData{CallID: id, Cost: cost}})
	}
	usage := func(id string, cost float64, in int) {
		elog.onEvent(&events.Event{Data: &events.ProviderCallUsageData{CallID: id, Cost: cost, InputTokens: in}})
	}

	completed("a", 0.001)
	completed("b", 0.002)
	usage("a", 0.004, 40)
	usage("c", 0.008, 80) // before call c's completion
	completed("c", 0.016)
	usage("", 0.032, 320)
	elog.end()

	assert.Equal(t, []string{
		"09:30:00.000  cost      call 1: $0.0010 (0 in / 0 out tokens); session total $0.0010",
		"09:30:00.000  cost      call 2: $0.0020 (0 in / 0 out tokens); session total $0.0030",
		"09:30:00.000  cost      late usage for call 1: +$0.0040 (40 in / 0 out tokens); session total $0.0070",
		"09:30:00.000  cost      call 3: $0.0160 (0 in / 0 out tokens); session total $0.0230",
		"09:30:00.000  cost      late usage for call 3: +$0.0080 (80 in / 0 out tokens); session total $0.0310",
		"09:30:00.000  cost      late usage for the session: +$0.0320 (320 in / 0 out tokens); session total $0.0630",
		"09:30:00.000  session   end: 3 provider calls, total cost $0.0630",
	}, out.lines())
}

// Usage for calls never seen to complete is printed at the session's end in
// the order it arrived, every run.
func TestShowcaseEventLog_PendingUsageIsPrintedInArrivalOrder(t *testing.T) {
	ids := []string{"k", "c", "x", "a", "q", "b", "z", "m"}
	var want []string
	for i := range ids {
		want = append(want, fmt.Sprintf(
			"09:30:00.000  cost      late usage for the session: +$0.0010 (%d in / 0 out tokens); session total $%.4f",
			i+1, float64(i+1)*0.001))
	}
	want = append(want, "09:30:00.000  session   end: 0 provider calls, total cost $0.0080")
	for range 20 { // map order would differ between runs
		out := &syncBuffer{}
		elog := newEventLog(out, statestore.NewMemoryStore(), conversationID)
		elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }
		for i, id := range ids {
			elog.onEvent(&events.Event{Data: &events.ProviderCallUsageData{CallID: id, Cost: 0.001, InputTokens: i + 1}})
		}
		elog.end()
		require.Equal(t, want, out.lines())
	}
}

// topicJudge is the mock judge bound to the pack's topic-judge: it finds the
// caller's words clean until offTopic is set.
type topicJudge struct {
	mu       sync.Mutex
	offTopic bool
}

func (j *topicJudge) set(offTopic bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.offTopic = offTopic
}

func (j *topicJudge) GetResponse(context.Context, mock.ResponseParams) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.offTopic {
		return `{"passed": false, "score": 0, "reasoning": "asks about Bruno without naming him"}`, nil
	}
	return `{"passed": true, "score": 1, "reasoning": "not about Bruno"}`, nil
}

func (j *topicJudge) GetTurn(ctx context.Context, params mock.ResponseParams) (*mock.Turn, error) {
	text, err := j.GetResponse(ctx, params)
	return &mock.Turn{Type: "text", Content: text}, err
}

func TestShowcaseEventLog_ScriptedSession(t *testing.T) {
	sess := mock.NewMockStreamSession().WithAutoRespond("The picture shows the number 47.").
		WithOutputAudio(providers.OutputAudio{Voice: "alloy", SampleRate: outputSampleRate, Family: "mock-live"})
	bus := events.NewEventBus()
	store := statestore.NewMemoryStore()
	out := &syncBuffer{}
	judge := &topicJudge{}
	elog := newEventLog(out, store, conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }
	elog.attach(bus)

	opts := append(logOptions(bus, store),
		sdk.WithProvider(&scriptedProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false), sess: sess}),
		sdk.WithStreamingConfig((&providerSetup{inputRate: 24000, encoding: "pcm16"}).streamingConfig()),
		sdk.WithPlayoutTracking(),
		// The pack's topic-judge, bound the way judgeProvider binds it, to a
		// mock model.
		sdk.WithNamedProvider(sdk.ProviderSpec{ID: judgeProviderKey, Type: "mock", Model: "judge",
			AdditionalConfig: map[string]any{"repository": mock.ResponseRepository(judge)}}),
	)
	conv, err := sdk.OpenDuplex(packPath, promptName, opts...)
	require.NoError(t, err)
	require.NoError(t, registerTools(conv))
	resp, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for c := range resp {
			elog.observe(c)
		}
	}()
	// The first audio opens the realtime session.
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)}}))
	r := &showcaseRun{t: t, sess: sess, out: out}

	// 1. A long story, talked over: the reply is cut and the stored reply
	// says how much of its 5 s of audio was heard.
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #1 "Once upon a time there was a lighthouse keeper."`,
		`09:30:00.000  barge-in  reply #1 cut (barge-in): heard_ms=N audio_ms=5000 played_fraction=F`,
		`09:30:00.000  cost      call 1: $0.0120 (900 in / 300 out tokens); session total $0.0120`,
		`09:30:00.000  interrupt Interrupted: stop playing the current reply`,
		`09:30:00.000  user      message.created #0 "tell me a long story"`,
	}, r.step(5, heard("tell me a long story"), said("Once upon a time there was a lighthouse keeper.", 5000),
		done(0.012, 900, 300), &providers.StreamChunk{Interrupted: true}))

	// 2. The output guardrail cuts a reply that says the banned word: the
	// caller is told to stop playing it, the guardrail's message follows as
	// text and plays as audio (a tone here: no TTS service), the turn ends
	// (safety), and the stored reply records what was heard before the cut.
	// The session goes on.
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #3 "Sorry, I can't say that word."`,
		`09:30:00.000  cost      call 2: $0.0050 (400 in / 100 out tokens); session total $0.0170`,
		`09:30:00.000  cue       the guardrail's message plays as audio (guardrail_message), not spoken by the model`,
		`09:30:00.000  guardrail no-pineapple blocked the assistant's reply ` +
			`(forbidden content found in stream: "pineapple"); the turn ends, the session continues`,
		`09:30:00.000  interrupt Interrupted: stop playing the current reply`,
		`09:30:00.000  interrupt turn ended (safety); text after the cut: "Sorry, I can't say that word."`,
		`09:30:00.000  playout   reply #3 cut (guardrail): heard_ms=N audio_ms=100 played_fraction=F`,
		`09:30:00.000  user      message.created #2 "say the word pineapple"`,
	}, r.step(8, heard("say the word pineapple"), said("Sure: ", 100), said("pineapple", 100),
		said(" is a tropical fruit.", 100), done(0.005, 400, 100)))

	// 3. The input guardrail judges what the caller said. Its message names
	// Bruno, and still plays: it is not the model's reply, so the output side
	// of no-bruno does not judge it.
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #5 "We don't talk about Bruno."`,
		`09:30:00.000  cost      call 3: $0.0030 (300 in / 50 out tokens); session total $0.0200`,
		`09:30:00.000  cue       the guardrail's message plays as audio (guardrail_message), not spoken by the model`,
		`09:30:00.000  guardrail no-bruno blocked the user's input ` +
			`(forbidden content found: turn 4 contains "Bruno"); the turn ends, the session continues`,
		`09:30:00.000  interrupt Interrupted: stop playing the current reply`,
		`09:30:00.000  interrupt turn ended (safety); text after the cut: "We don't talk about Bruno."`,
		`09:30:00.000  user      message.created #4 "Tell me about Bruno"`,
	}, r.step(7, heard("Tell me about Bruno"), said("Bruno is a fine name.", 100), done(0.003, 300, 50)))

	// 4. The LLM judge blocks a question about Bruno that does not name him.
	judge.set(true)
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #7 "We don't talk about Bruno."`,
		`09:30:00.000  cost      call 4: $0.0030 (300 in / 50 out tokens); session total $0.0230`,
		`09:30:00.000  cue       the guardrail's message plays as audio (guardrail_message), not spoken by the model`,
		`09:30:00.000  guardrail no-bruno-topic blocked the user's input ` +
			`(asks about Bruno without naming him); the turn ends, the session continues`,
		`09:30:00.000  interrupt Interrupted: stop playing the current reply`,
		`09:30:00.000  interrupt turn ended (safety); text after the cut: "We don't talk about Bruno."`,
		`09:30:00.000  user      message.created #6 "who is the uncle nobody mentions in Encanto"`,
	}, r.step(7, heard("who is the uncle nobody mentions in Encanto"), said("That would be", 100),
		done(0.003, 300, 50)))
	judge.set(false)

	// 5. The picture tool returns an image, which is sent to the model.
	toolUse := types.FinishReasonToolUse
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #11 "The picture shows the number 47."`,
		`09:30:00.000  assistant message.created #9 "" [calls show_picture]`,
		`09:30:00.000  cost      call 5: $0.0020 (200 in / 20 out tokens); session total $0.0250`,
		`09:30:00.000  cost      call 6: $0.0000 (0 in / 0 out tokens); session total $0.0250`,
		`09:30:00.000  tool      message.created #10 "" [image]`,
		`09:30:00.000  tool      show_picture called`,
		`09:30:00.000  tool      show_picture returned an image; image sent to the model (image/png)`,
		`09:30:00.000  user      message.created #8 "show me the picture"`,
	}, r.step(8, heard("show me the picture"), &providers.StreamChunk{
		ToolCalls:    []types.MessageToolCall{{ID: "call-1", Name: "show_picture", Args: []byte(`{}`)}},
		FinishReason: &toolUse,
		CostInfo:     &types.CostInfo{InputTokens: 200, OutputTokens: 20, TotalCost: 0.002},
	}))

	// 6. Typed text the input guardrail blocks never reaches the model; its
	// message is shown and plays as audio, with no cut (nothing was playing).
	require.NoError(t, conv.SendText(context.Background(), "Tell me about my friend Bruno."))
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #13 "We don't talk about Bruno."`,
		`09:30:00.000  cue       the guardrail's message plays as audio (guardrail_message), not spoken by the model`,
		`09:30:00.000  guardrail no-bruno blocked the user's input ` +
			`(forbidden content found: turn 12 contains "Bruno"); the turn ends, the session continues`,
		`09:30:00.000  user      message.created #12 "Tell me about my friend Bruno."`,
	}, r.step(4))
	assert.NotContains(t, sess.GetTexts(), "Tell me about my friend Bruno.", "the blocked text never reaches the model")

	require.NoError(t, conv.Close())
	bus.Close()
	elog.end()
	assert.Equal(t, []string{
		`09:30:00.000  session   end: 6 provider calls, total cost $0.0250`,
	}, r.step(1))
}

// The picture is a real PNG of the number, large enough to read.
func TestRenderNumberPNG(t *testing.T) {
	data, err := renderNumberPNG(pictureNumber)
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 336, img.Bounds().Dx())
	assert.Equal(t, 216, img.Bounds().Dy())
	// The top-left pixel of the "4" glyph's first stroke ("...#.") is black;
	// the margin is white.
	r, _, _, _ := img.At(24+3*24, 24).RGBA()
	assert.Equal(t, uint32(0), r)
	r, _, _, _ = img.At(0, 0).RGBA()
	assert.Equal(t, uint32(0xffff), r)

	_, err = renderNumberPNG("4x")
	assert.EqualError(t, err, `no glyph for 'x'`)
}

func TestSetupFor(t *testing.T) {
	oa, err := setupFor("openai")
	require.NoError(t, err)
	assert.Equal(t, "gpt-realtime", oa.model)
	assert.Equal(t, 4800, oa.streamingConfig().Config.ChunkSize)
	gm, err := setupFor("gemini")
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.8-live", gm.model)
	assert.Equal(t, 16000, gm.streamingConfig().Config.SampleRate)
	_, err = setupFor("claude")
	assert.EqualError(t, err, `unknown provider "claude" (want openai or gemini)`)
}

// -tts picks what a guardrail's message is heard as: a tone by default, or
// OpenAI TTS, which needs its key.
func TestUseTTS(t *testing.T) {
	setup, err := setupFor("gemini")
	require.NoError(t, err)
	require.NoError(t, setup.useTTS(""))
	opts, heard := setup.cueOptions()
	assert.Empty(t, opts)
	assert.Equal(t, "the session's own voice when it can speak out of band, else a short tone "+
		"(add -tts openai to hear the words)", heard)

	assert.EqualError(t, setup.useTTS("elevenlabs"), `unknown TTS service "elevenlabs" (want openai or gemini)`)
	t.Setenv("GEMINI_API_KEY", "")
	assert.EqualError(t, setup.useTTS("gemini"), "-tts gemini needs GEMINI_API_KEY")
	t.Setenv("GEMINI_API_KEY", "test-key")
	require.NoError(t, setup.useTTS("gemini"))
	_, heard = setup.cueOptions()
	assert.Equal(t, "the session's own voice when it can speak out of band, else gemini TTS", heard)
	t.Setenv("OPENAI_API_KEY", "")
	assert.EqualError(t, setup.useTTS("openai"), "-tts openai needs OPENAI_API_KEY")

	t.Setenv("OPENAI_API_KEY", "test-key")
	require.NoError(t, setup.useTTS("openai"))
	opts, heard = setup.cueOptions()
	assert.Len(t, opts, 1)
	assert.Equal(t, "the session's own voice when it can speak out of band, else openai TTS", heard)
}

// flagsFor is the flags for provider and mode, the rest at their defaults.
func flagsFor(provider, mode string) flags { return flags{provider: provider, mode: mode} }

// -mode picks the session: asm (the default) keeps the realtime model and
// -tts's meaning, vad switches to the text model with OpenAI STT and a TTS
// service -tts picks, each with the key it needs.
func TestResolve_Modes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("ELEVENLABS_API_KEY", "test-key")
	t.Setenv("CARTESIA_API_KEY", "")

	asm, err := resolve(flags{provider: "openai", mode: modeASM, model: "gpt-realtime-mini"})
	require.NoError(t, err)
	assert.Equal(t, modeASM, asm.mode)
	assert.Equal(t, "gpt-realtime-mini", asm.model, "-model is the realtime model in ASM mode")
	assert.Equal(t, "Realtime showcase - ASM mode: OpenAI Realtime (gpt-realtime-mini); guardrail messages: "+
		"the session's own voice when it can speak out of band, else a short tone (add -tts openai to hear the words)",
		asm.banner())

	vad, err := resolve(flags{provider: "gemini", mode: modeVAD, model: "gemini-2.5-pro"})
	require.NoError(t, err)
	assert.Equal(t, modeVAD, vad.mode)
	assert.Equal(t, "gemini-2.5-pro", vad.textModel, "-model is the text model in VAD mode")
	assert.Equal(t, "gemini-3.8-live", vad.model)
	assert.Equal(t, "Realtime showcase - VAD mode: Gemini gemini-2.5-pro, openai-whisper STT, openai TTS "+
		"(its default voice); guardrail messages: spoken by openai TTS, as every reply is",
		vad.banner())
	assert.Equal(t, vadSampleRate, vad.captureRate())
	assert.Equal(t, 24000, asm.captureRate(), "ASM captures at the realtime model's input rate")

	el, err := resolve(flags{provider: "openai", mode: modeVAD, tts: "elevenlabs", voice: "rachel"})
	require.NoError(t, err)
	assert.Equal(t, "Realtime showcase - VAD mode: OpenAI gpt-4.1-mini, openai-whisper STT, elevenlabs TTS "+
		"(voice rachel); guardrail messages: spoken by elevenlabs TTS, as every reply is",
		el.banner())
	assert.Equal(t, "rachel", el.vadConfig().Voice)
	assert.Equal(t, vadSampleRate, el.vadConfig().SampleRate)
	assert.Equal(t, "openai", el.callerTTS.Name(), "the scripted requests are spoken by OpenAI TTS")

	for _, tc := range []struct {
		f    flags
		want string
	}{
		{flagsFor("openai", "pipeline"), `unknown mode "pipeline" (want asm or vad)`},
		{flags{provider: "openai", mode: modeVAD, tts: "polly"},
			`unknown TTS service "polly" (want ` + strings.Join(tts.RegisteredTypes(), ", ") + ")"},
		{flags{provider: "openai", mode: modeVAD, stt: "deepgram"}, `unknown STT service "deepgram" (want ` + strings.Join(stt.RegisteredTypes(), ", ") + ")"},
		{flags{provider: "openai", mode: modeASM, stt: "openai"}, "-stt and -voice apply to -mode vad"},
		{flags{provider: "openai", mode: modeVAD, tts: "cartesia"}, "-tts cartesia needs CARTESIA_API_KEY"},
		{flags{provider: "openai", mode: modeASM, voice: "nova"}, "-stt and -voice apply to -mode vad"},
		{flags{provider: "openai", mode: modeASM, tts: "elevenlabs"},
			`unknown TTS service "elevenlabs" (want openai or gemini)`},
		{flags{provider: "openai", mode: modeASM, hold: "sentence"},
			`unknown -stream-hold "sentence" (want off, turn or segment)`},
		{flags{provider: "openai", mode: modeVAD, hold: "turn"}, "-stream-hold applies to -mode asm"},
	} {
		_, err := resolve(tc.f)
		assert.EqualError(t, err, tc.want, "%+v", tc.f)
	}

	held, err := resolve(flags{provider: "gemini", mode: modeASM, hold: "segment"})
	require.NoError(t, err)
	assert.Equal(t, sdk.RealtimeHoldSegment, held.streamHold)
	assert.Len(t, held.holdOptions(), 2, "the hold and its output judge")
	assert.True(t, strings.HasSuffix(held.banner(), "; replies held for the output judge: segment"), held.banner())
	off, err := resolve(flags{provider: "gemini", mode: modeASM, hold: "off"})
	require.NoError(t, err)
	assert.Empty(t, off.holdOptions(), "off holds nothing and adds no judge")

	t.Setenv("OPENAI_API_KEY", "")
	_, err = resolve(flagsFor("gemini", modeVAD))
	assert.EqualError(t, err, "-stt openai needs OPENAI_API_KEY")
}

// scriptSTT transcribes each turn as the next line of the script, at a fixed
// price.
type scriptSTT struct {
	mu    sync.Mutex
	lines []string
}

func (s *scriptSTT) next() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) == 0 {
		return ""
	}
	line := s.lines[0]
	s.lines = s.lines[1:]
	return line
}

func (s *scriptSTT) Name() string                      { return "script-stt" }
func (s *scriptSTT) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (s *scriptSTT) Pricing() *base.PricingDescriptor  { return nil }
func (s *scriptSTT) Validate() error                   { return nil }
func (s *scriptSTT) Init(context.Context) error        { return nil }
func (s *scriptSTT) HealthCheck(context.Context) error { return nil }
func (s *scriptSTT) Close() error                      { return nil }
func (s *scriptSTT) SupportedFormats() []string        { return []string{"pcm"} }

func (s *scriptSTT) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: s.next(), Cost: &types.CostInfo{
		TotalCost: 0.0005, Capability: "stt", Quantities: map[string]float64{"second": 5}}}, nil
}

func (s *scriptSTT) TranscribeBytes(context.Context, []byte, stt.TranscriptionConfig) (string, error) {
	return s.next(), nil
}

// pricedTTS synthesizes 100 ms of PCM at 24 kHz per reply, priced per
// character, and reports what it spoke. As a caller's voice it renders a
// loud square wave the VAD calls speech.
type pricedTTS struct{ speech time.Duration }

func (p *pricedTTS) Name() string                                         { return "priced-tts" }
func (p *pricedTTS) ImplName() string                                     { return "priced-tts" }
func (p *pricedTTS) ModelName() string                                    { return "" }
func (p *pricedTTS) SupportedVoices() []tts.Voice                         { return nil }
func (p *pricedTTS) SpokenText(text string, _ tts.SynthesisConfig) string { return text }
func (p *pricedTTS) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{Items: []base.PriceItem{{Unit: "character", Rate: 0.00001}}}
}

func (p *pricedTTS) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{{Name: "pcm", MIMEType: "audio/pcm", SampleRate: 24000, BitDepth: 16, Channels: 1}}
}

func (p *pricedTTS) Synthesize(_ context.Context, _ string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	if p.speech == 0 {
		return io.NopCloser(bytes.NewReader(make([]byte, 4800))), nil // 100 ms at 24 kHz
	}
	n := int(p.speech * time.Duration(cfg.Format.SampleRate) / time.Second)
	pcm := make([]byte, n*2)
	for i := range n {
		v := int16(12000)
		if i%8 < 4 {
			v = -12000
		}
		pcm[i*2], pcm[i*2+1] = byte(v), byte(v>>8)
	}
	return io.NopCloser(bytes.NewReader(pcm)), nil
}

// The scripted VAD mode without API keys: each request is synthesized and
// sent as speech, the session's VAD ends the turn, STT transcribes it, the
// input guardrail blocks the Bruno request before the text model, and the TTS
// service speaks every reply — the guardrail's message included — with the
// STT and TTS cost of each.
func TestShowcaseEventLog_ScriptedVADSession(t *testing.T) {
	bus := events.NewEventBus()
	store := statestore.NewMemoryStore()
	out := &syncBuffer{}
	elog := newEventLog(out, store, conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }
	elog.speechCosts = true
	elog.attach(bus)

	setup := &providerSetup{mode: modeVAD, tts: &pricedTTS{}, callerTTS: &pricedTTS{speech: 600 * time.Millisecond},
		stt: &scriptSTT{lines: []string{"Tell me about my friend Bruno.", "What is the capital of France?"}}}
	llm := mock.NewProviderWithRepository("llm", "text-model", false,
		mock.NewInMemoryMockRepository("The capital of France is Paris."))
	opts := append(logOptions(bus, store),
		sdk.WithProvider(llm),
		sdk.WithVADMode(setup.stt, setup.tts, setup.vadConfig()),
		sdk.WithNamedProvider(sdk.ProviderSpec{ID: judgeProviderKey, Type: "mock", Model: "judge",
			AdditionalConfig: map[string]any{"repository": mock.ResponseRepository(&topicJudge{})}}),
	)
	conv, err := sdk.OpenDuplex(packPath, promptName, opts...)
	require.NoError(t, err)
	require.NoError(t, registerTools(conv))
	resp, err := conv.Response()
	require.NoError(t, err)
	turns := make(chan reply, 2)
	go readReplies(resp, elog, turns)
	r := &showcaseRun{t: t, out: out, speechCosts: true}

	// One scripted request, spoken, and its reply.
	ask := func(line string) reply {
		require.NoError(t, send(setup, conv, elog, line))
		got, err := waitReply(turns)
		require.NoError(t, err)
		return got
	}

	blocked := ask("Tell me about my friend Bruno.")
	assert.Positive(t, blocked.firstAudio, "the first audio is timed from the request")
	blocked.firstAudio = 0
	assert.Equal(t, reply{finish: types.FinishReasonSafety, audio: 100 * time.Millisecond}, blocked)
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #1 "We don't talk about Bruno."`,
		`09:30:00.000  cost      STT for #0: $0.0005 (5 second); session total $T`,
		`09:30:00.000  cost      TTS for #1: $0.0003 (26 character); session total $T`,
		`09:30:00.000  guardrail no-bruno blocked the user's input ` +
			`(forbidden content found: turn 0 contains "Bruno"); the turn ends, the session continues`,
		`09:30:00.000  speech    TTS speaks "We don't talk about Bruno."`,
		`09:30:00.000  timing    first audio of the reply, 0.0 s after your words were transcribed`,
		`09:30:00.000  user      message.created #0 "Tell me about my friend Bruno."`,
		`09:30:00.000  you say   "Tell me about my friend Bruno." (0.6 s of speech, synthesized by priced-tts TTS)`,
	}, r.collect(8))
	require.NoError(t, listen(context.Background(), conv, blocked.audio))

	answered := ask("What is the capital of France?")
	assert.Equal(t, types.FinishReasonStop, answered.finish)
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #3 "The capital of France is Paris."`,
		`09:30:00.000  cost      STT for #2: $0.0005 (5 second); session total $T`,
		`09:30:00.000  cost      TTS for #3: $0.0003 (31 character); session total $T`,
		`09:30:00.000  cost      call 1: $0.0002 (10 in / 7 out tokens); session total $T`,
		`09:30:00.000  speech    TTS speaks "The capital of France is Paris."`,
		`09:30:00.000  timing    first audio of the reply, 0.0 s after your words were transcribed`,
		`09:30:00.000  user      message.created #2 "What is the capital of France?"`,
		`09:30:00.000  you say   "What is the capital of France?" (0.6 s of speech, synthesized by priced-tts TTS)`,
	}, r.collect(8))

	require.NoError(t, conv.Close())
	bus.Close()
	elog.end()
	assert.Equal(t, []string{
		`09:30:00.000  session   end: 1 provider calls, total cost (with STT and TTS) $0.0017`,
	}, r.collect(1))
}

// An error on Response(), such as a TTS service that could not speak a
// reply, is printed: the caller would otherwise hear nothing and see no why.
func TestShowcaseEventLog_PrintsResponseErrors(t *testing.T) {
	out := &syncBuffer{}
	elog := newEventLog(out, statestore.NewMemoryStore(), conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }

	elog.observe(providers.StreamChunk{Delta: "hello"})
	elog.observe(providers.StreamChunk{Error: errors.New("cartesia: The requested voice was not found.")})

	assert.Equal(t, []string{
		`09:30:00.000  error     cartesia: The requested voice was not found.`,
	}, out.lines())
}

// The log names the source that rendered a guardrail message's clip, as the
// library reports it: the session itself (its provider family) or the TTS
// service. Other TTS calls (VAD-mode replies) are not cue renders.
func TestEventLog_ReportsWhoRenderedTheCue(t *testing.T) {
	out := &syncBuffer{}
	elog := newEventLog(out, statestore.NewMemoryStore(), conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }
	for _, d := range []*events.TTSCallCompletedData{
		{CapabilityCallData: events.CapabilityCallData{Provider: "openai", Source: "cue"}, AudioSeconds: 1.84},
		{CapabilityCallData: events.CapabilityCallData{Provider: "openai-tts", Source: "reply"}, AudioSeconds: 3},
	} {
		elog.onEvent(&events.Event{Type: events.EventTTSCallCompleted, Data: d})
	}
	assert.Equal(t, []string{"09:30:00.000  cue       the guardrail's message was rendered by openai (1.8 s of audio)"},
		out.lines())
}
