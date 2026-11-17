package main

import (
	"bytes"
	"context"
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

func TestShowcaseEventLog_ScriptedSession(t *testing.T) {
	sess := mock.NewMockStreamSession().WithAutoRespond("The picture shows the number 47.")
	bus := events.NewEventBus()
	store := statestore.NewMemoryStore()
	out := &syncBuffer{}
	elog := newEventLog(out, store, conversationID)
	elog.now = func() time.Time { return time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC) }
	elog.attach(bus)

	opts := append(sessionOptions(bus, store),
		sdk.WithProvider(&scriptedProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false), sess: sess}),
		sdk.WithStreamingConfig((&providerSetup{inputRate: 24000, encoding: "pcm16"}).streamingConfig()),
		sdk.WithPlayoutTracking(),
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

	// 2. The output guardrail cuts a reply that says the banned word; the
	// turn ends with the guardrail's message and the session goes on.
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #3 "Sorry, I can't say that word."`,
		`09:30:00.000  cost      call 2: $0.0050 (400 in / 100 out tokens); session total $0.0170`,
		`09:30:00.000  guardrail no-pineapple blocked the assistant's reply ` +
			`(forbidden content found in stream: "pineapple"); the turn ends, the session continues`,
		`09:30:00.000  interrupt Interrupted: stop playing the current reply`,
		`09:30:00.000  user      message.created #2 "say the word pineapple"`,
	}, r.step(5, heard("say the word pineapple"), said("Sure: ", 100), said("pineapple", 100),
		said(" is a tropical fruit.", 100), done(0.005, 400, 100)))

	// 3. The input guardrail judges what the caller said.
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #5 "Let's not talk about bananas."`,
		`09:30:00.000  cost      call 3: $0.0030 (300 in / 50 out tokens); session total $0.0200`,
		`09:30:00.000  guardrail no-banana blocked the user's input ` +
			`(forbidden content found: turn 4 contains "banana"); the turn ends, the session continues`,
		`09:30:00.000  interrupt Interrupted: stop playing the current reply`,
		`09:30:00.000  user      message.created #4 "I like banana bread"`,
	}, r.step(5, heard("I like banana bread"), said("Banana bread is great.", 100), done(0.003, 300, 50)))

	// 4. The picture tool returns an image, which is sent to the model.
	toolUse := types.FinishReasonToolUse
	assert.Equal(t, []string{
		`09:30:00.000  assistant message.created #7 "" [calls show_picture]`,
		`09:30:00.000  assistant message.created #9 "The picture shows the number 47."`,
		`09:30:00.000  cost      call 4: $0.0020 (200 in / 20 out tokens); session total $0.0220`,
		`09:30:00.000  cost      call 5: $0.0000 (0 in / 0 out tokens); session total $0.0220`,
		`09:30:00.000  tool      message.created #8 "" [image]`,
		`09:30:00.000  tool      show_picture called`,
		`09:30:00.000  tool      show_picture returned an image; image sent to the model (image/png)`,
		`09:30:00.000  user      message.created #6 "show me the picture"`,
	}, r.step(8, heard("show me the picture"), &providers.StreamChunk{
		ToolCalls:    []types.MessageToolCall{{ID: "call-1", Name: "show_picture", Args: []byte(`{}`)}},
		FinishReason: &toolUse,
		CostInfo:     &types.CostInfo{InputTokens: 200, OutputTokens: 20, TotalCost: 0.002},
	}))

	require.NoError(t, conv.Close())
	bus.Close()
	elog.end()
	assert.Equal(t, []string{
		`09:30:00.000  session   end: 5 provider calls, total cost $0.0220`,
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
