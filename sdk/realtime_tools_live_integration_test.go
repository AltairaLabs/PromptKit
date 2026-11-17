//go:build integration

package sdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// Live verification of realtime (ASM) tool rounds against real providers:
// OpenAI Realtime and Gemini Live, and, where the behavior is shared, the
// cascaded VAD path and a text turn. Every case drives the public API
// (OpenDuplex / Open, SendText / Send, OnTool*, the event bus) and asserts
// what a host observes: the handler's arguments and call ID, the events, and
// a reply that can only come from the tool's result reaching the model.
//
// Run (keys loaded from .env into the test process only):
//
//	go -C sdk test -tags integration -run 'TestLiveRealtimeTools' -count=3 -v .
//
// Models: OPENAI_REALTIME_MODEL (default gpt-realtime), GEMINI_LIVE_MODEL
// (default gemini-3.8-live), OPENAI_TEXT_MODEL (default gpt-4o-mini).

// rtlTimeout bounds one wait for the model.
const rtlTimeout = 60 * time.Second

// rtlWeatherResult is what get_weather returns. "tangerine" is a word the
// model cannot produce from the prompt alone, so a reply carrying it shows the
// result reached the model.
var rtlWeatherResult = map[string]any{"condition": "tangerine haze", "celsius": 17}

const rtlWeatherPrompt = "What is the weather in Paris right now? Call the get_weather tool for Paris, " +
	"then tell me the condition it reports in one short sentence."

const rtlWeatherPackJSON = `{
	"id": "rt-live-tools",
	"version": "1.0.0",
	"description": "Live realtime tool verification",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a terse assistant. For any weather question you must call the get_weather tool; never guess. If a tool call fails or is refused, tell the user the exact error text it returned. Reply in plain text, one short sentence.",
			"tools": ["get_weather"]
		}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get the current weather for a city",
			"parameters": {
				"type": "object",
				"properties": {"city": {"type": "string", "description": "City name"}},
				"required": ["city"]
			}
		}
	}
}`

// rtlRealtime is one realtime provider under test.
type rtlRealtime struct {
	name      string
	envKey    string
	provider  func() providers.Provider
	streamCfg func() *providers.StreamingInputConfig
	// updatesTools reports whether the session takes tools mid-session.
	updatesTools bool
	// toolRoundUsage reports whether the provider reports a tool-call turn's
	// usage on that turn. Gemini Live reports it with the reply that follows
	// (one server turn), so the tool round's provider.call.completed carries
	// no tokens and the reply's carries both.
	toolRoundUsage bool
}

func rtlEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func rtlRealtimeProviders() []rtlRealtime {
	return []rtlRealtime{
		{
			name:   "openai-realtime",
			envKey: "OPENAI_API_KEY",
			provider: func() providers.Provider {
				return openai.NewProvider("openai-realtime", rtlEnvOr("OPENAI_REALTIME_MODEL", "gpt-realtime"),
					"https://api.openai.com", providers.ProviderDefaults{}, false)
			},
			streamCfg: func() *providers.StreamingInputConfig {
				return &providers.StreamingInputConfig{
					Config: types.StreamingMediaConfig{
						Type: types.ContentTypeAudio, ChunkSize: 4800,
						SampleRate: 24000, Channels: 1, BitDepth: 16, Encoding: "pcm16",
					},
					Metadata: map[string]interface{}{"modalities": []string{"text"}},
				}
			},
			updatesTools:   true,
			toolRoundUsage: true,
		},
		{
			name:   "gemini-live",
			envKey: "GEMINI_API_KEY",
			provider: func() providers.Provider {
				return gemini.NewProvider("gemini-live", rtlEnvOr("GEMINI_LIVE_MODEL", "gemini-3.8-live"),
					"https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)
			},
			streamCfg: func() *providers.StreamingInputConfig {
				return &providers.StreamingInputConfig{
					Config: types.StreamingMediaConfig{
						Type: types.ContentTypeAudio, ChunkSize: 3200,
						SampleRate: 16000, Channels: 1, BitDepth: 16, Encoding: "pcm_linear16",
					},
					Metadata: map[string]interface{}{
						"response_modalities": []string{rtlEnvOr("GEMINI_LIVE_MODALITY", "AUDIO")},
					},
				}
			},
		},
	}
}

// rtlEachRealtime runs fn once per realtime provider whose key is set.
func rtlEachRealtime(t *testing.T, fn func(t *testing.T, rt rtlRealtime)) {
	t.Helper()
	for _, rt := range rtlRealtimeProviders() {
		t.Run(rt.name, func(t *testing.T) {
			if os.Getenv(rt.envKey) == "" {
				t.Skipf("%s not set", rt.envKey)
			}
			fn(t, rt)
		})
	}
}

func rtlWritePack(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "live.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// rtlOpenDuplex opens a realtime conversation on rt.
func rtlOpenDuplex(t *testing.T, rt rtlRealtime, packJSON string, opts ...sdk.Option) *sdk.Conversation {
	t.Helper()
	base := []sdk.Option{
		sdk.WithProvider(rt.provider()),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(rt.streamCfg()),
	}
	conv, err := sdk.OpenDuplex(rtlWritePack(t, packJSON), "chat", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// rtlTextProvider is the OpenAI chat provider text turns and VAD run on.
func rtlTextProvider(t *testing.T) providers.Provider {
	t.Helper()
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "openai-text", Type: "openai", Model: rtlEnvOr("OPENAI_TEXT_MODEL", "gpt-4o-mini"),
		BaseURL:  "https://api.openai.com/v1",
		Defaults: providers.ProviderDefaults{MaxTokens: 300},
	})
	require.NoError(t, err)
	return p
}

// rtlOpenText opens a text conversation on the OpenAI chat provider.
func rtlOpenText(t *testing.T, opts ...sdk.Option) *sdk.Conversation {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	base := []sdk.Option{sdk.WithProvider(rtlTextProvider(t)), sdk.WithSkipSchemaValidation()}
	conv, err := sdk.Open(rtlWritePack(t, rtlWeatherPackJSON), "chat", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// rtlStream collects a duplex conversation's Response() channel.
type rtlStream struct {
	mu        sync.Mutex
	text      strings.Builder
	errs      []error
	pending   []tools.PendingToolExecution
	toolCalls []string
	closed    chan struct{}
}

func rtlWatch(t *testing.T, conv *sdk.Conversation) *rtlStream {
	t.Helper()
	ch, err := conv.Response()
	require.NoError(t, err)
	s := &rtlStream{closed: make(chan struct{})}
	debug := os.Getenv("RTL_DEBUG") != ""
	go func() {
		defer close(s.closed)
		for c := range ch {
			if debug {
				t.Logf("chunk: delta=%q content=%q finish=%v tools=%d pending=%d err=%v cost=%+v meta=%v",
					c.Delta, rtlTrunc(c.Content), c.FinishReason, len(c.ToolCalls), len(c.PendingTools),
					c.Error, c.CostInfo, rtlKeys(c.Metadata))
			}
			s.mu.Lock()
			s.text.WriteString(c.Delta)
			if c.Error != nil {
				s.errs = append(s.errs, c.Error)
			}
			s.pending = append(s.pending, c.PendingTools...)
			for _, tc := range c.ToolCalls {
				s.toolCalls = append(s.toolCalls, tc.Name)
			}
			s.mu.Unlock()
		}
	}()
	return s
}

func rtlKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok && len(s) < 60 {
			out = append(out, k+"="+s)
			continue
		}
		out = append(out, k)
	}
	return out
}

func rtlTrunc(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func (s *rtlStream) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.String()
}

func (s *rtlStream) Errors() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.errs...)
}

func (s *rtlStream) Pending() []tools.PendingToolExecution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tools.PendingToolExecution(nil), s.pending...)
}

func (s *rtlStream) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// textSince returns the reply text after offset (a prior len(Text())), lower-cased.
func (s *rtlStream) textSince(offset int) string {
	all := s.Text()
	if offset > len(all) {
		offset = len(all)
	}
	return strings.ToLower(all[offset:])
}

// waitText waits for any of words (lower-case) in the text after offset.
func (s *rtlStream) waitText(offset int, timeout time.Duration, words ...string) bool {
	return rtlWaitUntil(timeout, func() bool {
		got := s.textSince(offset)
		for _, w := range words {
			if strings.Contains(got, w) {
				return true
			}
		}
		return false
	})
}

func rtlWaitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}

// rtlEvents collects every event on a bus.
type rtlEvents struct {
	mu  sync.Mutex
	evs []*events.Event
}

func rtlBus(t *testing.T) (*events.EventBus, *rtlEvents) {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	c := &rtlEvents{}
	bus.SubscribeAll(func(e *events.Event) {
		c.mu.Lock()
		c.evs = append(c.evs, e)
		c.mu.Unlock()
	})
	return bus, c
}

func (c *rtlEvents) ofType(tp events.EventType) []*events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*events.Event
	for _, e := range c.evs {
		if e.Type == tp {
			out = append(out, e)
		}
	}
	return out
}

func (c *rtlEvents) toolEvents(tp events.EventType, tool string) []*events.ToolCallEventData {
	var out []*events.ToolCallEventData
	for _, e := range c.ofType(tp) {
		if d, ok := e.Data.(*events.ToolCallEventData); ok && (tool == "" || d.ToolName == tool) {
			out = append(out, d)
		}
	}
	return out
}

func (c *rtlEvents) providerCompleted() []*events.ProviderCallCompletedData {
	var out []*events.ProviderCallCompletedData
	for _, e := range c.ofType(events.EventProviderCallCompleted) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			out = append(out, d)
		}
	}
	return out
}

func (c *rtlEvents) providerStarted() []*events.ProviderCallStartedData {
	var out []*events.ProviderCallStartedData
	for _, e := range c.ofType(events.EventProviderCallStarted) {
		if d, ok := e.Data.(*events.ProviderCallStartedData); ok {
			out = append(out, d)
		}
	}
	return out
}

func (c *rtlEvents) summary() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	counts := map[events.EventType]int{}
	for _, e := range c.evs {
		counts[e.Type]++
	}
	return fmt.Sprint(counts)
}

// rtlCalls records the calls a tool handler receives.
type rtlCalls struct {
	mu    sync.Mutex
	calls []sdk.ToolCall
}

func (r *rtlCalls) handler(result any, err error) func(context.Context, sdk.ToolCall) (any, error) {
	return func(_ context.Context, call sdk.ToolCall) (any, error) {
		r.mu.Lock()
		r.calls = append(r.calls, call)
		r.mu.Unlock()
		return result, err
	}
}

func (r *rtlCalls) all() []sdk.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sdk.ToolCall(nil), r.calls...)
}

func (r *rtlCalls) count() int { return len(r.all()) }

// rtlAssertParisCall asserts a get_weather call was for Paris with a provider call ID.
func rtlAssertParisCall(t *testing.T, call sdk.ToolCall) {
	t.Helper()
	city, _ := call.Args["city"].(string)
	assert.Contains(t, strings.ToLower(city), "paris", "get_weather args: %v", call.Args)
	assert.NotEmpty(t, call.CallID, "the handler was not given the provider's call ID")
}

// rtlAssertRoundEvents asserts the events of a tool round whose call ran with
// callID: tool.call.started/completed name it, and the provider call that made
// it, and the one its result continued, completed with tokens and cost.
func rtlAssertRoundEvents(t *testing.T, ev *rtlEvents, callID string, toolRoundUsage bool) {
	t.Helper()
	var started *events.ToolCallEventData
	require.Truef(t, rtlWaitUntil(5*time.Second, func() bool {
		for _, d := range ev.toolEvents(events.EventToolCallStarted, "get_weather") {
			if d.CallID == callID {
				started = d
				return true
			}
		}
		return false
	}), "no tool.call.started for call %s (events %s)", callID, ev.summary())
	assert.Equal(t, "paris", strings.ToLower(fmt.Sprint(started.Args["city"])))
	require.NotEmpty(t, started.ProviderCallID)

	require.Truef(t, rtlWaitUntil(5*time.Second, func() bool {
		for _, d := range ev.toolEvents(events.EventToolCallCompleted, "get_weather") {
			if d.CallID == callID {
				return d.Status == "complete"
			}
		}
		return false
	}), "no complete tool.call.completed for call %s (events %s)", callID, ev.summary())

	var toolRound *events.ProviderCallCompletedData
	require.Truef(t, rtlWaitUntil(5*time.Second, func() bool {
		for _, d := range ev.providerCompleted() {
			if d.CallID == started.ProviderCallID {
				toolRound = d
				return true
			}
		}
		return false
	}), "no provider.call.completed for the tool call's provider call (events %s)", ev.summary())
	assert.GreaterOrEqual(t, toolRound.ToolCallCount, 1)
	if toolRoundUsage {
		assert.Positive(t, toolRound.InputTokens, "provider.call.completed input tokens")
		assert.Positive(t, toolRound.OutputTokens, "provider.call.completed output tokens")
		assert.Positive(t, toolRound.Cost, "provider.call.completed cost")
	}

	startedIDs := map[string]bool{}
	for _, d := range ev.providerStarted() {
		startedIDs[d.CallID] = true
	}
	assert.True(t, startedIDs[started.ProviderCallID], "provider.call.started for the tool call's provider call")

	// The reply to the tool result is a later provider call of the same tool
	// turn, completed with cost.
	require.Truef(t, rtlWaitUntil(10*time.Second, func() bool {
		for _, d := range ev.providerCompleted() {
			if d.Round == toolRound.Round+1 && d.ToolCallCount == 0 && d.Cost > 0 &&
				d.InputTokens > 0 && d.OutputTokens > 0 {
				return true
			}
		}
		return false
	}), "no provider.call.completed with cost for the reply after the tool result (events %s)", ev.summary())
	for _, d := range ev.providerCompleted() {
		t.Logf("provider.call.completed round=%d tools=%d in=%d out=%d cost=%.6f finish=%s",
			d.Round, d.ToolCallCount, d.InputTokens, d.OutputTokens, d.Cost, d.FinishReason)
	}
}

// ---------------------------------------------------------------------------
// Case 1: a basic tool round.
// ---------------------------------------------------------------------------

func TestLiveRealtimeTools_BasicRound(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus))
		calls := &rtlCalls{}
		conv.OnToolCall("get_weather", calls.handler(rtlWeatherResult, nil))
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(), rtlWeatherPrompt))
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return calls.count() > 0 }),
			"get_weather never ran; reply so far %q errors %v", st.Text(), st.Errors())
		call := calls.all()[0]
		rtlAssertParisCall(t, call)
		require.Truef(t, st.waitText(0, rtlTimeout, "tangerine"),
			"the reply never used the tool result: %q", st.Text())
		rtlAssertRoundEvents(t, ev, call.CallID, rt.toolRoundUsage)
		assert.Empty(t, st.Errors())
		t.Logf("reply: %q", st.Text())
	})
}

func TestLiveRealtimeTools_BasicRound_Text(t *testing.T) {
	bus, ev := rtlBus(t)
	conv := rtlOpenText(t, sdk.WithEventBus(bus))
	calls := &rtlCalls{}
	conv.OnToolCall("get_weather", calls.handler(rtlWeatherResult, nil))

	ctx, cancel := context.WithTimeout(context.Background(), rtlTimeout)
	defer cancel()
	resp, err := conv.Send(ctx, rtlWeatherPrompt)
	require.NoError(t, err)
	require.Equal(t, 1, calls.count())
	call := calls.all()[0]
	rtlAssertParisCall(t, call)
	assert.Contains(t, strings.ToLower(resp.Text()), "tangerine")
	rtlAssertRoundEvents(t, ev, call.CallID, true)
}

// rtlSpeak streams PCM16 speech at real time through SendChunk, then trailing
// silence long enough to close the VAD turn.
func rtlSpeak(t *testing.T, conv *sdk.Conversation, pcm []byte, rate int, trailing time.Duration) {
	t.Helper()
	const frame = 20 * time.Millisecond
	bytesPerFrame := int(float64(rate)*frame.Seconds()) * 2
	send := func(b []byte) {
		require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: b},
		}))
		time.Sleep(frame)
	}
	for off := 0; off < len(pcm); off += bytesPerFrame {
		send(pcm[off:min(off+bytesPerFrame, len(pcm))])
	}
	silence := make([]byte, bytesPerFrame)
	for d := time.Duration(0); d < trailing; d += frame {
		send(silence)
	}
}

func TestLiveRealtimeTools_BasicRound_VAD(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	const rate = 24000
	ttsSvc := tts.NewOpenAI(key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, err := ttsSvc.Synthesize(ctx, "What is the weather in Paris right now? Use the weather tool.",
		tts.SynthesisConfig{Voice: tts.VoiceAlloy, Format: tts.FormatPCM16, Model: tts.ModelTTS1, Speed: 1.0})
	require.NoError(t, err)
	pcm, err := io.ReadAll(rc)
	_ = rc.Close()
	require.NoError(t, err)

	bus, ev := rtlBus(t)
	conv, err := sdk.OpenDuplex(rtlWritePack(t, rtlWeatherPackJSON), "chat",
		sdk.WithProvider(rtlTextProvider(t)),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
		sdk.WithVADMode(stt.NewOpenAI(key), ttsSvc, &sdk.VADModeConfig{
			SilenceDuration: 500 * time.Millisecond, MinSpeechDuration: 200 * time.Millisecond,
			MaxTurnDuration: 15 * time.Second, SampleRate: rate, Language: "en", Voice: "alloy", Speed: 1.0,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	calls := &rtlCalls{}
	conv.OnToolCall("get_weather", calls.handler(rtlWeatherResult, nil))
	st := rtlWatch(t, conv)

	rtlSpeak(t, conv, pcm, rate, time.Second)
	require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return calls.count() > 0 }),
		"get_weather never ran; reply so far %q errors %v events %s", st.Text(), st.Errors(), ev.summary())
	call := calls.all()[0]
	rtlAssertParisCall(t, call)
	require.Truef(t, st.waitText(0, rtlTimeout, "tangerine"), "the reply never used the tool result: %q", st.Text())
	rtlAssertRoundEvents(t, ev, call.CallID, true)
	t.Logf("VAD reply: %q", st.Text())
}

// ---------------------------------------------------------------------------
// Case 2: a before-hook denial reaches the model (#2696).
// ---------------------------------------------------------------------------

type rtlDenyHook struct{ tool, reason string }

func (h rtlDenyHook) Name() string { return "deny-" + h.tool }

func (h rtlDenyHook) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
	if req.Name == h.tool {
		return hooks.Deny(h.reason)
	}
	return hooks.Allow
}

func (h rtlDenyHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

var rtlDeny = rtlDenyHook{tool: "get_weather", reason: "weather lookups are suspended under policy KESTREL"}

func TestLiveRealtimeTools_BeforeHookDenial(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus), sdk.WithToolHook(rtlDeny))
		calls := &rtlCalls{}
		conv.OnToolCall("get_weather", calls.handler(rtlWeatherResult, nil))
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(), rtlWeatherPrompt))
		require.Truef(t, st.waitText(0, rtlTimeout, "kestrel"),
			"the model was not told the hook's reason: %q (events %s)", st.Text(), ev.summary())
		assert.Zero(t, calls.count(), "a denied call ran")
		t.Logf("reply: %q events %s", st.Text(), ev.summary())
	})
}

func TestLiveRealtimeTools_BeforeHookDenial_Text(t *testing.T) {
	conv := rtlOpenText(t, sdk.WithToolHook(rtlDeny))
	calls := &rtlCalls{}
	conv.OnToolCall("get_weather", calls.handler(rtlWeatherResult, nil))
	ctx, cancel := context.WithTimeout(context.Background(), rtlTimeout)
	defer cancel()
	resp, err := conv.Send(ctx, rtlWeatherPrompt)
	require.NoError(t, err)
	assert.Zero(t, calls.count(), "a denied call ran")
	// What the model is sent for the call: the denial with the hook's reason.
	// (The reply itself is gpt-4o-mini's to word; it often paraphrases.)
	var results []string
	for _, m := range resp.TurnMessages() {
		if m.ToolResult != nil {
			results = append(results, m.ToolResult.GetTextContent())
		}
	}
	require.NotEmpty(t, results, "the turn has no tool result")
	assert.Equal(t, "Tool get_weather blocked by hook: "+rtlDeny.reason, results[0])
	t.Logf("reply: %q", resp.Text())
}

// ---------------------------------------------------------------------------
// Case 3: a blocklisted tool is refused.
// ---------------------------------------------------------------------------

func TestLiveRealtimeTools_BlocklistRefuses(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus),
			sdk.WithToolPolicy(&rtpipeline.ToolPolicy{Blocklist: []string{"get_weather"}}))
		calls := &rtlCalls{}
		conv.OnToolCall("get_weather", calls.handler(rtlWeatherResult, nil))
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(), rtlWeatherPrompt))
		require.Truef(t, st.waitText(0, rtlTimeout, "block", "policy"),
			"the model was not told the call was blocked: %q (events %s)", st.Text(), ev.summary())
		assert.Zero(t, calls.count(), "a blocklisted call ran")
		assert.NotContains(t, st.textSince(0), "tangerine")
		t.Logf("reply: %q", st.Text())
	})
}

// ---------------------------------------------------------------------------
// Case 4: the caller reaches a CallExecutor.
// ---------------------------------------------------------------------------

type rtlCallExecutor struct {
	mu    sync.Mutex
	calls []tools.ToolCall
}

func (e *rtlCallExecutor) Name() string { return "rtl-recording" }

func (e *rtlCallExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("Execute must not be used when ExecuteCall is available")
}

func (e *rtlCallExecutor) ExecuteCall(
	_ context.Context, _ *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	out, _ := json.Marshal(rtlWeatherResult)
	return out, nil, nil
}

func (e *rtlCallExecutor) recorded() []tools.ToolCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tools.ToolCall(nil), e.calls...)
}

var rtlCaller = types.Caller{ID: "user-42", Claims: map[string]any{"tier": "gold"}}

func rtlAssertCallerCall(t *testing.T, ev *rtlEvents, exec *rtlCallExecutor) {
	t.Helper()
	got := exec.recorded()
	require.NotEmpty(t, got)
	assert.Equal(t, rtlCaller, got[0].Caller)
	require.NotEmpty(t, got[0].ID)
	require.Truef(t, rtlWaitUntil(5*time.Second, func() bool {
		for _, d := range ev.toolEvents(events.EventToolCallStarted, "get_weather") {
			if d.CallID == got[0].ID {
				return true
			}
		}
		return false
	}), "the executor's call ID %q is not the provider's (events %s)", got[0].ID, ev.summary())
}

func TestLiveRealtimeTools_CallerReachesCallExecutor(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus), sdk.WithCaller(rtlCaller))
		exec := &rtlCallExecutor{}
		conv.OnToolExecutor("get_weather", exec)
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(), rtlWeatherPrompt))
		require.Truef(t, st.waitText(0, rtlTimeout, "tangerine"),
			"the reply never used the executor's result: %q (events %s)", st.Text(), ev.summary())
		rtlAssertCallerCall(t, ev, exec)
	})
}

func TestLiveRealtimeTools_CallerReachesCallExecutor_Text(t *testing.T) {
	bus, ev := rtlBus(t)
	conv := rtlOpenText(t, sdk.WithEventBus(bus), sdk.WithCaller(rtlCaller))
	exec := &rtlCallExecutor{}
	conv.OnToolExecutor("get_weather", exec)
	ctx, cancel := context.WithTimeout(context.Background(), rtlTimeout)
	defer cancel()
	resp, err := conv.Send(ctx, rtlWeatherPrompt)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(resp.Text()), "tangerine")
	rtlAssertCallerCall(t, ev, exec)
}

// ---------------------------------------------------------------------------
// Case 5: HITL hold, resolve, continue.
// ---------------------------------------------------------------------------

func TestLiveRealtimeTools_ApprovalHoldThenResolve(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON)
		execs := &rtlCalls{}
		var checked []sdk.ToolCall
		var mu sync.Mutex
		conv.OnToolAsyncCall("get_weather",
			func(_ context.Context, call sdk.ToolCall) sdktools.PendingResult {
				mu.Lock()
				checked = append(checked, call)
				mu.Unlock()
				return sdktools.PendingResult{Reason: "needs_approval", Message: "approve the weather lookup"}
			},
			execs.handler(rtlWeatherResult, nil))
		st := rtlWatch(t, conv)
		ctx := context.Background()

		require.NoError(t, conv.SendText(ctx, rtlWeatherPrompt))
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return len(st.Pending()) > 0 }),
			"no pending call surfaced; reply %q errors %v", st.Text(), st.Errors())
		held := st.Pending()[0]
		assert.Equal(t, "get_weather", held.ToolName)
		require.NotNil(t, held.PendingInfo)
		assert.Equal(t, "needs_approval", held.PendingInfo.Reason)
		assert.Contains(t, strings.ToLower(fmt.Sprint(held.Args["city"])), "paris")
		assert.Zero(t, execs.count(), "a held call ran before approval")

		offset := len(st.Text())
		_, err := conv.ResolveTool(ctx, held.CallID)
		require.NoError(t, err)
		require.NoError(t, conv.ContinueDuplex(ctx))
		require.Equal(t, 1, execs.count())
		assert.Equal(t, held.CallID, execs.all()[0].CallID)
		require.Truef(t, st.waitText(offset, rtlTimeout, "tangerine"),
			"the approved result never reached the model: %q errors %v", st.textSince(offset), st.Errors())
		assert.Empty(t, st.Errors())
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, checked, 1)
		assert.Equal(t, held.CallID, checked[0].CallID)
	})
}

// rtlRewriteHook replaces every tool result with a fixed text and records the
// requests it is given.
type rtlRewriteHook struct {
	replacement string
	mu          sync.Mutex
	reqs        []hooks.ToolRequest
}

func (h *rtlRewriteHook) Name() string { return "rewrite" }

func (h *rtlRewriteHook) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (h *rtlRewriteHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (h *rtlRewriteHook) RewriteResult(_ context.Context, req hooks.ToolRequest, _ string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs = append(h.reqs, req)
	return h.replacement, nil
}

// A client tool's answer delivered with ContinueDuplex reaches the model as
// the conversation's rewriter rewrites it, with the call the model made.
func TestLiveRealtimeTools_ClientToolAnswerIsRewritten(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		rw := &rtlRewriteHook{replacement: `{"condition": "violet drizzle", "celsius": 9}`}
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithToolHook(rw))
		conv.DeferClientTool("get_weather")
		st := rtlWatch(t, conv)
		ctx := context.Background()

		require.NoError(t, conv.SendText(ctx, rtlWeatherPrompt))
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return len(st.Pending()) > 0 }),
			"no client call surfaced; reply %q errors %v", st.Text(), st.Errors())
		held := st.Pending()[0]
		require.Equal(t, "get_weather", held.ToolName)

		offset := len(st.Text())
		require.NoError(t, conv.SendToolResult(ctx, held.CallID, rtlWeatherResult))
		require.NoError(t, conv.ContinueDuplex(ctx))
		require.Truef(t, st.waitText(offset, rtlTimeout, "violet"),
			"the rewritten answer never reached the model: %q errors %v", st.textSince(offset), st.Errors())
		assert.NotContains(t, strings.ToLower(st.textSince(offset)), "tangerine")
		rw.mu.Lock()
		defer rw.mu.Unlock()
		require.Len(t, rw.reqs, 1)
		assert.Equal(t, "get_weather", rw.reqs[0].Name)
		assert.Equal(t, held.CallID, rw.reqs[0].CallID)
		assert.Contains(t, strings.ToLower(string(rw.reqs[0].Args)), "paris")
	})
}

// ---------------------------------------------------------------------------
// Case 6: capability tools (memory) are offered and callable (#2697).
// ---------------------------------------------------------------------------

func TestLiveRealtimeTools_MemoryToolsCallable(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		store := memory.NewInMemoryStore()
		scope := map[string]string{"user_id": "alice"}

		// Session one stores a fact.
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus), sdk.WithMemory(store, scope))
		st := rtlWatch(t, conv)
		require.NoError(t, conv.SendText(context.Background(),
			"Please remember that my favourite colour is teal: store it with the memory__remember tool, "+
				"then confirm in a few words."))
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool {
			return len(ev.toolEvents(events.EventToolCallCompleted, "memory__remember")) > 0 ||
				len(ev.toolEvents(events.EventToolCallFailed, "memory__remember")) > 0
		}), "memory__remember never ran; reply %q events %s", st.Text(), ev.summary())
		for _, d := range ev.toolEvents(events.EventToolCallFailed, "memory__remember") {
			t.Errorf("memory__remember failed: %s (args %v)", d.Error, d.Args)
		}
		mems, err := store.List(context.Background(), scope, memory.ListOptions{})
		require.NoError(t, err)
		require.NotEmpty(t, mems, "nothing was stored")
		assert.Contains(t, strings.ToLower(mems[0].Content), "teal")
		_ = conv.Close()

		// Session two recalls it.
		bus2, ev2 := rtlBus(t)
		conv2 := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus2), sdk.WithMemory(store, scope))
		st2 := rtlWatch(t, conv2)
		require.NoError(t, conv2.SendText(context.Background(),
			"What is my favourite colour? Look it up with the memory__recall tool, then tell me."))
		require.Truef(t, st2.waitText(0, rtlTimeout, "teal"),
			"the recalled memory never reached the reply: %q events %s", st2.Text(), ev2.summary())
		require.NotEmpty(t, ev2.toolEvents(events.EventToolCallCompleted, "memory__recall"),
			"memory__recall did not complete (events %s)", ev2.summary())
		assert.Empty(t, ev2.toolEvents(events.EventToolCallFailed, ""))
	})
}

// ---------------------------------------------------------------------------
// Case 7: a skill grant mid-session.
// ---------------------------------------------------------------------------

const rtlSkillPackJSON = `{
	"id": "rt-live-skill-grant",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a support agent. Follow the user's instructions about which tools to call, in order. Reply in plain text, one short sentence.",
			"tools": ["lookup_order"]
		}
	},
	"tools": {
		"lookup_order": {
			"name": "lookup_order",
			"description": "Look up an order",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}
		},
		"refund": {
			"name": "refund",
			"description": "Issue a refund for an order",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}
		}
	}
}`

func rtlWriteRefundSkill(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "refund-processing")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := "---\nname: refund-processing\ndescription: How to process refunds\n" +
		"allowed-tools:\n  - refund\n---\n\nTo refund an order, call the refund tool with the order id.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600))
	return root
}

func TestLiveRealtimeTools_SkillGrantMidSession(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlSkillPackJSON, sdk.WithEventBus(bus), sdk.WithSkillsDir(rtlWriteRefundSkill(t)))
		refunds := &rtlCalls{}
		conv.OnToolCall("refund", refunds.handler(map[string]any{"status": "refunded", "ref": "RF-ORCHID"}, nil))
		conv.OnTool("lookup_order", func(map[string]any) (any, error) { return "shipped", nil })
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(),
			"I need a refund for order ORD-1. First activate the refund-processing skill with the "+
				"skill__activate tool. Once it is active, call the refund tool with id ORD-1 and tell me "+
				"the refund reference it returns."))
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool {
			return len(ev.toolEvents(events.EventToolCallCompleted, "skill__activate")) > 0
		}), "skill__activate never completed; reply %q events %s", st.Text(), ev.summary())

		if rt.updatesTools {
			require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return refunds.count() > 0 }),
				"the granted refund tool never ran; reply %q events %s errors %v", st.Text(), ev.summary(), st.Errors())
			assert.Equal(t, "ORD-1", refunds.all()[0].Args["id"])
			require.Truef(t, st.waitText(0, rtlTimeout, "orchid"), "the refund result never reached the reply: %q", st.Text())
			// The session was given the granted tool: the rounds after the
			// activation hold one more tool than the first.
			counts := rtlOfferedCounts(ev)
			require.NotEmpty(t, counts)
			assert.Greater(t, counts[len(counts)-1], counts[0], "tools held per round: %v", counts)
			return
		}
		defer func() {
			counts := rtlOfferedCounts(ev)
			for _, c := range counts {
				assert.Equal(t, counts[0], c, "a session that cannot change its tools changed them: %v", counts)
			}
		}()
		// A session that fixes its tools at setup: the granted tool is never
		// run. Give the model time to try.
		rtlWaitUntil(20*time.Second, func() bool { return refunds.count() > 0 })
		assert.Zero(t, refunds.count(), "a tool granted after setup ran on a session that cannot hold it")
		t.Logf("model tool calls %v; reply %q; errors %v", st.toolCallNames(), st.Text(), st.Errors())
	})
}

// rtlOfferedCounts is how many tools the session held at the start of each
// provider call, in order.
func rtlOfferedCounts(ev *rtlEvents) []int {
	var out []int
	for _, d := range ev.providerStarted() {
		out = append(out, d.ToolCount)
	}
	return out
}

func (s *rtlStream) toolCallNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.toolCalls...)
}

// ---------------------------------------------------------------------------
// Case 8: a tiny MaxCostUSD closes the session after the first paid turn.
// ---------------------------------------------------------------------------

func TestLiveRealtimeTools_MaxCostClosesSession(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus),
			sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxCostUSD: 0.0000001}))
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(), "Say hello in three words."))
		select {
		case <-st.closed:
		case <-time.After(rtlTimeout):
			t.Fatalf("Response() never closed; errors %v events %s", st.Errors(), ev.summary())
		}
		errs := st.Errors()
		require.Len(t, errs, 1, "errors: %v", errs)
		assert.Contains(t, errs[0].Error(), "cost budget exceeded")

		// No further paid turn: one provider call completed, and nothing starts later.
		time.Sleep(3 * time.Second)
		before := len(ev.providerStarted())
		_ = conv.SendText(context.Background(), "Say goodbye.")
		time.Sleep(5 * time.Second)
		assert.Len(t, ev.providerCompleted(), 1, "provider calls completed (events %s)", ev.summary())
		assert.Equal(t, before, len(ev.providerStarted()), "a provider call started after the session closed")
	})
}

// ---------------------------------------------------------------------------
// Case 9: a failing tool's error reaches the model; the session goes on.
// ---------------------------------------------------------------------------

func TestLiveRealtimeTools_ToolErrorKeepsSessionOpen(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON, sdk.WithEventBus(bus))
		calls := &rtlCalls{}
		conv.OnToolCall("get_weather",
			calls.handler(nil, errors.New("weather service unavailable during incident OSPREY")))
		st := rtlWatch(t, conv)
		ctx := context.Background()

		require.NoError(t, conv.SendText(ctx, rtlWeatherPrompt))
		require.Truef(t, st.waitText(0, rtlTimeout, "osprey"),
			"the tool's error never reached the reply: %q (events %s)", st.Text(), ev.summary())
		require.GreaterOrEqual(t, calls.count(), 1)
		// A handler error is a completed call with status failed (as on a text turn).
		var statuses []string
		for _, d := range ev.toolEvents(events.EventToolCallCompleted, "get_weather") {
			statuses = append(statuses, d.Status)
		}
		for range ev.toolEvents(events.EventToolCallFailed, "get_weather") {
			statuses = append(statuses, "tool.call.failed")
		}
		require.NotEmpty(t, statuses, "no tool.call.completed/failed for get_weather")
		assert.NotContains(t, statuses, "complete", "a failing call reported as complete")
		t.Logf("get_weather outcomes: %v", statuses)

		// Let the model finish its reply before the next input.
		time.Sleep(3 * time.Second)
		require.False(t, st.isClosed(), "Response() closed after a tool error")
		offset := len(st.Text())
		require.NoError(t, conv.SendText(ctx, "Now reply with only the single word pineapple."))
		require.Truef(t, st.waitText(offset, rtlTimeout, "pineapple"),
			"the next input was not answered: %q", st.textSince(offset))
		assert.Empty(t, st.Errors())
	})
}

// ---------------------------------------------------------------------------
// Case 10: a registered tool the session was not offered never runs (#2713).
// ---------------------------------------------------------------------------

// rtlUnofferedPackJSON declares issue_refund in the pack but not on the
// prompt, so it is registered and never offered. The system prompt tells the
// model to call it, which a model sometimes does by name.
const rtlUnofferedPackJSON = `{
	"id": "rt-live-unoffered",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a terse assistant. For any refund request call the issue_refund function with the order_id, even if it is not listed among your tools. If a tool call fails or is refused, tell the user the exact error text it returned. Reply in plain text, one short sentence.",
			"tools": ["get_weather"]
		}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get the current weather for a city",
			"parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
		},
		"issue_refund": {
			"name": "issue_refund",
			"description": "Issue a refund for an order",
			"parameters": {"type": "object", "properties": {"order_id": {"type": "string"}}, "required": ["order_id"]}
		}
	}
}`

func TestLiveRealtimeTools_UnofferedToolNeverRuns(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlUnofferedPackJSON, sdk.WithEventBus(bus))
		refunds := &rtlCalls{}
		conv.OnToolCall("issue_refund", refunds.handler(map[string]any{"status": "refunded"}, nil))
		st := rtlWatch(t, conv)

		// The refusal is reported as a failed call; whether the model makes
		// the call at all is its choice, so ask up to rtlUnofferedAttempts times.
		const refusal = "Tool issue_refund was not run: it is not available this turn"
		attempted := func() bool { return len(ev.toolEvents(events.EventToolCallFailed, "issue_refund")) > 0 }
		for i := 0; i < rtlUnofferedAttempts && !attempted(); i++ {
			completed := len(ev.providerCompleted())
			offset := len(st.Text())
			require.NoError(t, conv.SendText(context.Background(),
				fmt.Sprintf("Call issue_refund for order ORD-7%d now, then tell me what it returned.", i)))
			require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return rtlReplyEnded(ev, completed) }),
				"the turn never ended; reply %q errors %v events %s", st.textSince(offset), st.Errors(), ev.summary())
			t.Logf("attempt %d: reply %q", i+1, st.textSince(offset))
		}
		if !attempted() {
			assert.Zero(t, refunds.count(), "a tool the session was not offered ran")
			t.Skipf("%s never called the unoffered tool in %d attempts; nothing to verify", rt.name, rtlUnofferedAttempts)
		}
		failed := ev.toolEvents(events.EventToolCallFailed, "issue_refund")
		require.Error(t, failed[0].Error)
		assert.Equal(t, refusal, failed[0].Error.Error())
		assert.Zero(t, refunds.count(), "a tool the session was not offered ran")
		// What the model was told is the tool response the stage sent the
		// session, built from this call's tool result: its text, flagged as an
		// error. The model's own wording of it is its choice.
		var sent *types.MessageToolResult
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool {
			sent = rtlToolResult(conv, failed[0].CallID)
			return sent != nil
		}), "no tool result for call %s in the history", failed[0].CallID)
		assert.Equal(t, refusal, sent.GetTextContent(), "the session was not sent the refusal")
		assert.Equal(t, refusal, sent.Error, "the refusal was not sent as an error")
		t.Logf("reply: %q", st.Text())
	})
}

// rtlToolResult returns the tool result for callID in conv's history, or nil.
func rtlToolResult(conv *sdk.Conversation, callID string) *types.MessageToolResult {
	for _, m := range conv.Messages(context.Background()) {
		if m.ToolResult != nil && m.ToolResult.ID == callID {
			return m.ToolResult
		}
	}
	return nil
}

// rtlUnofferedAttempts is how many times the unoffered-tool case asks.
const rtlUnofferedAttempts = 3

// rtlReplyEnded reports whether a provider call after the first completed
// ones made no tool calls: the reply that ends the turn.
func rtlReplyEnded(ev *rtlEvents, completed int) bool {
	done := ev.providerCompleted()
	for _, d := range done[min(completed, len(done)):] {
		if d.ToolCallCount == 0 {
			return true
		}
	}
	return false
}
