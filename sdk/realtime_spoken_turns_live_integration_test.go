//go:build integration

package sdk_test

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const rtlChatPackJSON = `{
	"id": "rt-live-chat",
	"version": "1.0.0",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You are a helpful voice assistant. Answer in one short sentence."}
	}
}`

// rtlMic is a live microphone: it sends a 20 ms frame every 20 ms for the
// whole session, speech when there is some queued and silence otherwise.
type rtlMic struct {
	t     *testing.T
	conv  *sdk.Conversation
	frame int
	mu    sync.Mutex
	queue []byte
	stop  chan struct{}
	done  chan struct{}
}

func rtlStartMic(t *testing.T, conv *sdk.Conversation, rate int) *rtlMic {
	m := &rtlMic{t: t, conv: conv, frame: rate / 50 * 2, stop: make(chan struct{}), done: make(chan struct{})}
	go m.run()
	t.Cleanup(m.close)
	return m
}

func (m *rtlMic) run() {
	defer close(m.done)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-tick.C:
		}
		m.mu.Lock()
		n := min(m.frame, len(m.queue))
		out := make([]byte, m.frame)
		copy(out, m.queue[:n])
		m.queue = m.queue[n:]
		m.mu.Unlock()
		_ = m.conv.SendChunk(context.Background(), &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: out},
		})
	}
}

// say queues speech; the mic sends it, then goes back to silence.
func (m *rtlMic) say(pcm []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, pcm...)
}

func (m *rtlMic) close() {
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	<-m.done
}

// rtlSynthesize speaks text as 24 kHz PCM16 with OpenAI TTS.
func rtlSynthesize(t *testing.T, svc tts.Service, text string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, err := svc.Synthesize(ctx, text,
		tts.SynthesisConfig{Voice: tts.VoiceAlloy, Format: tts.FormatPCM16, Model: tts.ModelTTS1, Speed: 1.0})
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	pcm, err := io.ReadAll(rc)
	require.NoError(t, err)
	return pcm
}

// rtlCreated returns the message.created contents of role, in index order.
func rtlCreated(ev *rtlEvents, role string) []string {
	byIndex := map[int]string{}
	maxIndex := -1
	for _, e := range ev.ofType(events.EventMessageCreated) {
		d := e.Data.(*events.MessageCreatedData)
		if d.Role != role {
			continue
		}
		byIndex[d.Index] = d.GetContent()
		maxIndex = max(maxIndex, d.Index)
	}
	var out []string
	for i := 0; i <= maxIndex; i++ {
		if c, ok := byIndex[i]; ok {
			out = append(out, c)
		}
	}
	return out
}

// Two spoken turns through a mic that sends silence the whole time, between
// and after the turns: each turn is one user message with its own words, so
// silence frames neither open a turn nor move a late transcript to the next
// one.
func TestLiveRealtimeSpokenTurns_ContinuousMicSilence(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set (OpenAI TTS speaks the turns)")
	}
	svc := tts.NewOpenAI(key)
	first := rtlSynthesize(t, svc, "Please say the word banana.")
	second := rtlSynthesize(t, svc, "Thank you. Now please say the word cherry.")

	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		rate := rt.streamCfg().Config.SampleRate
		turn1, turn2 := first, second
		if rate != 24000 {
			var err error
			turn1, err = audio.ResamplePCM16(first, 24000, rate)
			require.NoError(t, err)
			turn2, err = audio.ResamplePCM16(second, 24000, rate)
			require.NoError(t, err)
		}
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlChatPackJSON, sdk.WithEventBus(bus))
		st := rtlWatch(t, conv)
		mic := rtlStartMic(t, conv, rate)

		mic.say(turn1)
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return len(rtlCreated(ev, "assistant")) >= 1 }),
			"no first reply; reply so far %q errors %v", st.Text(), st.Errors())
		time.Sleep(4 * time.Second) // silence, while a late transcript can land

		mic.say(turn2)
		require.Truef(t, rtlWaitUntil(rtlTimeout, func() bool { return len(rtlCreated(ev, "assistant")) >= 2 }),
			"no second reply; reply so far %q errors %v", st.Text(), st.Errors())
		time.Sleep(4 * time.Second)
		mic.close()

		users := rtlCreated(ev, "user")
		t.Logf("user messages: %q; assistant messages: %q", users, rtlCreated(ev, "assistant"))
		require.Len(t, users, 2, "one user message per spoken turn")
		assert.Contains(t, strings.ToLower(users[0]), "banana")
		assert.NotContains(t, strings.ToLower(users[0]), "cherry")
		assert.Contains(t, strings.ToLower(users[1]), "cherry")
		assert.NotContains(t, strings.ToLower(users[1]), "banana")
	})
}
