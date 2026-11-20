package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
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
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
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
}

// step drives one exchange and returns the lines it printed, sorted: the
// bus, the Response() reader and the store poll print from their own
// goroutines, so the order within a step is not fixed.
func (r *showcaseRun) step(want int, chunks ...*providers.StreamChunk) []string {
	r.t.Helper()
	for _, c := range chunks {
		r.sess.EmitChunk(c)
	}
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
		out[i] = heardRe.ReplaceAllString(l, "heard_ms=N $1 played_fraction=F")
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

	opts := append(sessionOptions(bus, store),
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
	assert.Equal(t, "a short tone (no TTS service; add -tts openai to hear the words)", heard)

	assert.EqualError(t, setup.useTTS("elevenlabs"), `unknown TTS service "elevenlabs" (want openai or gemini)`)
	t.Setenv("GEMINI_API_KEY", "")
	assert.EqualError(t, setup.useTTS("gemini"), "-tts gemini needs GEMINI_API_KEY")
	t.Setenv("GEMINI_API_KEY", "test-key")
	require.NoError(t, setup.useTTS("gemini"))
	_, heard = setup.cueOptions()
	assert.Equal(t, "spoken by gemini TTS", heard)
	t.Setenv("OPENAI_API_KEY", "")
	assert.EqualError(t, setup.useTTS("openai"), "-tts openai needs OPENAI_API_KEY")

	t.Setenv("OPENAI_API_KEY", "test-key")
	require.NoError(t, setup.useTTS("openai"))
	opts, heard = setup.cueOptions()
	assert.Len(t, opts, 1)
	assert.Equal(t, "spoken by openai TTS", heard)
}

// On OpenAI Realtime the session speaks a guardrail's message itself, in its
// own voice, out of band: -tts is only the fallback.
func TestCueOptions_OpenAISpeaksInItsOwnVoice(t *testing.T) {
	setup, err := setupFor("openai")
	require.NoError(t, err)
	require.NoError(t, setup.useTTS(""))
	opts, heard := setup.cueOptions()
	assert.Empty(t, opts)
	assert.Equal(t, "spoken by the session in its own voice (out of band)", heard)

	t.Setenv("OPENAI_API_KEY", "test-key")
	require.NoError(t, setup.useTTS("openai"))
	opts, heard = setup.cueOptions()
	assert.Len(t, opts, 1, "the TTS service stays configured as the fallback")
	assert.Equal(t, "spoken by the session in its own voice (out of band; openai TTS if it cannot)", heard)
}
