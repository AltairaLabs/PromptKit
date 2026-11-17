//go:build integration

package sdk_test

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Live verification of barge-in after a realtime reply finished generating
// but is still playing (#2702), on OpenAI Realtime and Gemini Live, and of a
// Gemini Live tool turn's cost (#2706).
//
// Run (keys loaded from .env into the test process only):
//
//	go -C sdk test -tags integration -run 'TestLiveRealtimePlayout' -count=1 -v .

const rtpStoryPrompt = "Tell me a long, detailed story about a lighthouse keeper and the sea. " +
	"Speak for about thirty seconds without stopping."

const rtpPackJSON = `{
	"id": "rt-live-playout",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a storyteller. When asked for a story, tell a long one."
		}
	}
}`

// rtpCase is one realtime provider in audio mode, with the input rate its
// caller audio is sent at.
type rtpCase struct {
	name      string
	envKey    string
	inputRate int
	encoding  string
	provider  func() providers.Provider
	metadata  map[string]interface{}
}

func rtpCases() []rtpCase {
	return []rtpCase{
		{
			name: "openai-realtime", envKey: "OPENAI_API_KEY", inputRate: 24000, encoding: "pcm16",
			provider: func() providers.Provider {
				return openai.NewProvider("openai-realtime", rtlEnvOr("OPENAI_REALTIME_MODEL", "gpt-realtime"),
					"https://api.openai.com", providers.ProviderDefaults{}, false)
			},
			metadata: map[string]interface{}{"modalities": []string{"audio"}},
		},
		{
			name: "gemini-live", envKey: "GEMINI_API_KEY", inputRate: 16000, encoding: "pcm_linear16",
			provider: func() providers.Provider {
				return gemini.NewProvider("gemini-live", rtlEnvOr("GEMINI_LIVE_MODEL", "gemini-3.8-live"),
					"https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)
			},
			metadata: map[string]interface{}{"response_modalities": []string{"AUDIO"}},
		},
	}
}

// rtpPlayback tracks a reply's audio as a real-time player would: the
// playout end, from the first chunk on, and the interruption.
type rtpPlayback struct {
	mu          sync.Mutex
	until       time.Time
	audio       time.Duration
	turnDone    bool
	interrupted time.Time
	sampleRate  int
}

func (p *rtpPlayback) watch(t *testing.T, conv *sdk.Conversation) {
	t.Helper()
	ch, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for c := range ch {
			p.mu.Lock()
			if c.MediaData != nil && len(c.MediaData.Data) > 0 && c.MediaData.SampleRate > 0 {
				d := time.Duration(len(c.MediaData.Data)/2) * time.Second / time.Duration(c.MediaData.SampleRate)
				start := p.until
				if now := time.Now(); now.After(start) {
					start = now
				}
				p.until = start.Add(d)
				p.audio += d
			}
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				p.turnDone = true
			}
			if c.Interrupted && p.interrupted.IsZero() {
				p.interrupted = time.Now()
			}
			p.mu.Unlock()
		}
	}()
}

func (p *rtpPlayback) snapshot() (until time.Time, audio time.Duration, turnDone bool, interrupted time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.until, p.audio, p.turnDone, p.interrupted
}

// rtpSpeech synthesizes text as PCM16 at rate.
func rtpSpeech(t *testing.T, text string, rate int) []byte {
	t.Helper()
	svc := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, err := svc.Synthesize(ctx, text, tts.SynthesisConfig{Voice: tts.VoiceAlloy, Format: tts.FormatPCM16})
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	pcm, err := io.ReadAll(rc)
	require.NoError(t, err)
	if rate != 24000 {
		pcm, err = audio.ResamplePCM16(pcm, 24000, rate)
		require.NoError(t, err)
	}
	return pcm
}

// rtpMic is an always-on microphone: it sends a 20 ms frame of PCM16 at
// real time, silence unless speech is queued. A realtime session needs the
// stream (Gemini Live ends a turn on the silence it hears; each frame resets
// the pipeline's idle timer).
type rtpMic struct {
	mu      sync.Mutex
	pending []byte
}

func (m *rtpMic) say(pcm []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = append(m.pending, pcm...)
}

func (m *rtpMic) run(ctx context.Context, conv *sdk.Conversation, rate int) {
	frame := rate / 50 * 2
	for ctx.Err() == nil {
		m.mu.Lock()
		n := min(frame, len(m.pending))
		data := append([]byte(nil), m.pending[:n]...)
		m.pending = m.pending[n:]
		m.mu.Unlock()
		data = append(data, make([]byte, frame-n)...)
		sendCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := conv.SendChunk(sendCtx, &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: data},
		})
		cancel()
		if err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLiveRealtimePlayout_BargeInWhileTheFinishedReplyPlays(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set (needed for the caller's synthesized speech)")
	}
	for _, rc := range rtpCases() {
		t.Run(rc.name, func(t *testing.T) {
			if os.Getenv(rc.envKey) == "" {
				t.Skipf("%s not set", rc.envKey)
			}
			speech := rtpSpeech(t, "Wait, stop. I have a question.", rc.inputRate)
			conv, err := sdk.OpenDuplex(rtlWritePack(t, rtpPackJSON), "chat",
				sdk.WithProvider(rc.provider()),
				sdk.WithSkipSchemaValidation(),
				sdk.WithStreamingConfig(&providers.StreamingInputConfig{
					Config: types.StreamingMediaConfig{
						Type: types.ContentTypeAudio, ChunkSize: 3200,
						SampleRate: rc.inputRate, Channels: 1, BitDepth: 16, Encoding: rc.encoding,
					},
					Metadata: rc.metadata,
				}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			p := &rtpPlayback{}
			p.watch(t, conv)
			mic := &rtpMic{}
			micCtx, micStop := context.WithCancel(context.Background())
			t.Cleanup(micStop)
			go mic.run(micCtx, conv, rc.inputRate)

			require.NoError(t, conv.SendText(context.Background(), rtpStoryPrompt))
			require.Truef(t, rtlWaitUntil(90*time.Second, func() bool {
				_, _, done, _ := p.snapshot()
				return done
			}), "the reply never finished generating")
			until, delivered, _, interrupted := p.snapshot()
			left := time.Until(until)
			t.Logf("reply generated: %v of audio delivered, %v still to play", delivered, left)
			require.True(t, interrupted.IsZero(), "interrupted before the caller spoke")
			if left < 4*time.Second {
				// Observed on gemini-3.8-live: it delivered a 49 s reply at about
				// real time, so the turn completed as its audio ran out and there
				// was no window after generation to barge into.
				t.Skipf("the provider delivered the reply at about real time (%v of audio, %v left at "+
					"its completion): no playout window after generation to test", delivered, left)
			}

			// The caller talks over the reply that is still playing.
			mic.say(speech)
			require.Truef(t, rtlWaitUntil(15*time.Second, func() bool {
				_, _, _, at := p.snapshot()
				return !at.IsZero()
			}), "no Interrupted chunk reached Response() while the finished reply played")
			_, _, _, at := p.snapshot()
			assert.Truef(t, at.Before(until), "the interruption (%v) came after the reply's playout end (%v)",
				at.Format(time.StampMilli), until.Format(time.StampMilli))
			t.Logf("interrupted %v before the reply's estimated playout end", until.Sub(at))
		})
	}
}

// A Gemini Live two-tool chain is charged in full: one provider.call.completed
// per call, each tool call's carrying its own usage (reported after its tool
// calls), and the conversation's messages adding up to the events' total.
func TestLiveRealtimePlayout_GeminiToolChainCost(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	rt := rtlRealtimeProviders()[1]
	bus, ev := rtlBus(t)
	conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus))
	calls := &rtlCalls{}
	conv.OnToolCall("get_weather", func(ctx context.Context, call sdk.ToolCall) (any, error) {
		time.Sleep(300 * time.Millisecond) // a tool that takes a moment
		return calls.handler(rtlWeatherResult, nil)(ctx, call)
	})
	st := rtlWatch(t, conv)

	require.NoError(t, conv.SendText(context.Background(),
		"First call get_weather for Rome. After you get that result, call get_weather for Oslo. "+
			"Then tell me both conditions in one sentence."))
	require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return calls.count() >= 2 }),
		"the chain never made two calls; reply %q", st.Text())
	require.Truef(t, st.waitText(0, rtlTimeout, "tangerine"), "the reply never used the results: %q", st.Text())
	require.True(t, rtlWaitUntil(10*time.Second, func() bool { return len(ev.providerCompleted()) >= 3 }))
	time.Sleep(2 * time.Second) // let the reply's event land

	perCall := map[string]int{}
	var eventCost float64
	for _, d := range ev.providerCompleted() {
		perCall[d.CallID]++
		eventCost += d.Cost
		t.Logf("provider.call.completed round=%d tools=%d in=%d out=%d cost=%.6f finish=%s",
			d.Round, d.ToolCallCount, d.InputTokens, d.OutputTokens, d.Cost, d.FinishReason)
	}
	for id, n := range perCall {
		assert.Equal(t, 1, n, "one provider.call.completed for call %s", id)
	}
	toolCallCost := map[string]float64{}
	for _, e := range ev.ofType(events.EventProviderCallUsage) {
		d := e.Data.(*events.ProviderCallUsageData)
		eventCost += d.Cost
		toolCallCost[d.CallID] += d.Cost
		t.Logf("provider.call.usage round=%d in=%d out=%d cost=%.6f", d.Round, d.InputTokens, d.OutputTokens, d.Cost)
	}
	for _, d := range ev.providerCompleted() {
		if d.ToolCallCount > 0 {
			assert.Positive(t, d.Cost+toolCallCost[d.CallID], "the tool call's own usage is on its call")
		}
	}
	var messageCost float64
	for _, m := range conv.Messages(context.Background()) {
		if m.CostInfo != nil {
			messageCost += m.CostInfo.TotalCost
		}
	}
	t.Logf("events total $%.6f, messages total $%.6f", eventCost, messageCost)
	assert.Positive(t, eventCost)
	assert.InDelta(t, eventCost, messageCost, 1e-9, "the messages carry every cost the events report")
}
