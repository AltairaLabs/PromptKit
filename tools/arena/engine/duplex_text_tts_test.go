package engine

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
)

// fakeTTSService is a minimal base.TTSProvider that returns a fixed PCM audio
// payload for any synthesis call. It captures the last text it was asked to
// synthesise so tests can assert the helper plumbed the right input through.
type fakeTTSService struct {
	payload  []byte
	lastText string
}

func (f *fakeTTSService) Name() string                        { return "fake-tts" }
func (f *fakeTTSService) Type() base.ProviderType             { return base.ProviderTypeTTS }
func (f *fakeTTSService) Pricing() *base.PricingDescriptor    { return nil }
func (f *fakeTTSService) Validate() error                     { return nil }
func (f *fakeTTSService) Init(_ context.Context) error        { return nil }
func (f *fakeTTSService) HealthCheck(_ context.Context) error { return nil }
func (f *fakeTTSService) Close() error                        { return nil }

func (f *fakeTTSService) SynthesizeTTS(_ context.Context, req base.TTSRequest) (base.TTSStream, error) {
	f.lastText = req.Text
	return newFakeTTSStream(f.payload), nil
}

// fakeTTSStream wraps a byte slice as a base.TTSStream for testing.
type fakeTTSStream struct {
	ch chan audio.Chunk
}

func newFakeTTSStream(data []byte) base.TTSStream {
	ch := make(chan audio.Chunk, 1)
	s := &fakeTTSStream{ch: ch}
	go func() {
		defer close(ch)
		if len(data) > 0 {
			ch <- audio.Chunk{Data: data}
		}
	}()
	return s
}

func (s *fakeTTSStream) Chunks() <-chan audio.Chunk { return s.ch }
func (s *fakeTTSStream) Cost() *types.CostInfo      { return nil }
func (s *fakeTTSStream) Close() error {
	for range s.ch { //nolint:revive // drain
	}
	return nil
}

// newRegistryWithFakeTTS builds a self-play registry whose TTS sub-registry has
// a pre-registered fake TTS service for the given provider name.
func newRegistryWithFakeTTS(provider string, payload []byte) (*selfplay.Registry, *fakeTTSService) {
	ttsRegistry := selfplay.NewTTSRegistry()
	fake := &fakeTTSService{payload: payload}
	ttsRegistry.Register(provider, fake)

	reg := selfplay.NewRegistryWithTTS(
		nil,
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		[]config.SelfPlayRoleGroup{},
		ttsRegistry,
	)
	return reg, fake
}

func TestResolveTTS_PerTurnWins(t *testing.T) {
	de := &DuplexConversationExecutor{}

	turn := &config.TurnDefinition{TTS: &config.TTSConfig{Provider: "turn", Voice: "v1"}}
	scenario := &config.Scenario{TTS: &config.TTSConfig{Provider: "scenario", Voice: "v2"}}
	cfg := &config.Config{Defaults: config.Defaults{TTS: &config.TTSConfig{Provider: "arena", Voice: "v3"}}}

	got := de.resolveTTS(turn, scenario, cfg)
	require.NotNil(t, got)
	assert.Equal(t, "turn", got.Provider, "turn-level TTS must win over scenario and arena defaults")
}

func TestResolveTTS_ScenarioFillsIn(t *testing.T) {
	de := &DuplexConversationExecutor{}

	turn := &config.TurnDefinition{} // no TTS
	scenario := &config.Scenario{TTS: &config.TTSConfig{Provider: "scenario", Voice: "v2"}}
	cfg := &config.Config{Defaults: config.Defaults{TTS: &config.TTSConfig{Provider: "arena", Voice: "v3"}}}

	got := de.resolveTTS(turn, scenario, cfg)
	require.NotNil(t, got)
	assert.Equal(t, "scenario", got.Provider, "scenario TTS must win over arena defaults when turn is unset")
}

func TestResolveTTS_ArenaDefaultsFillIn(t *testing.T) {
	de := &DuplexConversationExecutor{}

	turn := &config.TurnDefinition{}
	scenario := &config.Scenario{}
	cfg := &config.Config{Defaults: config.Defaults{TTS: &config.TTSConfig{Provider: "arena", Voice: "v3"}}}

	got := de.resolveTTS(turn, scenario, cfg)
	require.NotNil(t, got)
	assert.Equal(t, "arena", got.Provider, "arena defaults must fill in when turn and scenario have no TTS")
}

func TestResolveTTS_NoneConfigured(t *testing.T) {
	de := &DuplexConversationExecutor{}

	turn := &config.TurnDefinition{}
	scenario := &config.Scenario{}
	cfg := &config.Config{}

	got := de.resolveTTS(turn, scenario, cfg)
	assert.Nil(t, got, "no TTS at any level should return nil")
}

func TestResolveTTS_NilCfgIsSafe(t *testing.T) {
	de := &DuplexConversationExecutor{}

	got := de.resolveTTS(&config.TurnDefinition{}, &config.Scenario{}, nil)
	assert.Nil(t, got)
}

func TestProcessScriptedTextDuplexTurn_ErrorsWithoutTTS(t *testing.T) {
	reg, _ := newRegistryWithFakeTTS("openai", []byte{0x00, 0x01})
	de := &DuplexConversationExecutor{selfPlayRegistry: reg}

	req := &ConversationRequest{
		Scenario: &config.Scenario{ID: "s1"},
		Config:   &config.Config{},
	}
	turn := &config.TurnDefinition{Role: "user", Content: "hello world"}

	in := make(chan stage.StreamElement, 4)
	out := make(chan stage.StreamElement)
	close(out)

	err := de.processScriptedTextDuplexTurn(context.Background(), req, turn, 0, in, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TTS configuration required")
}

// TestStreamTextAsAudio_HappyPath verifies that the helper synthesises the
// supplied text via TTS, emits a user message element with both text and audio
// parts, and stamps a turn ID into the typed metadata.
func TestStreamTextAsAudio_HappyPath(t *testing.T) {
	pcmPayload := bytes.Repeat([]byte{0x00, 0x10}, 16) // 32 bytes of fake PCM
	reg, fake := newRegistryWithFakeTTS("fake", pcmPayload)
	de := &DuplexConversationExecutor{selfPlayRegistry: reg}

	// pumpTTSChunks emits the TTS body plus ~2s of silence-tail PCM chunks
	// (~100 elements at 16kHz). The test must drain inputChan concurrently
	// so streamTextAsAudio doesn't deadlock on a full channel.
	in := make(chan stage.StreamElement, 16)
	out := make(chan stage.StreamElement, 1)

	// Emit a "complete" stream element so the response collector returns nil
	// instead of blocking. EndOfStream + a Message with Content satisfies
	// ProcessResponseElement's ResponseActionComplete path.
	complete := stage.StreamElement{
		EndOfStream: true,
		Message:     &types.Message{Role: "assistant", Content: "ok"},
	}
	out <- complete
	// Don't close — we want the collector to terminate via the complete action.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ttsCfg := &config.TTSConfig{Provider: "fake", Voice: "v1", SampleRate: 16000}
	turnMeta := map[string]any{
		"persona":   "support-agent",
		"self_play": true,
	}

	collected := make([]stage.StreamElement, 0, 256)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for elem := range in {
			collected = append(collected, elem)
		}
	}()

	err := de.streamTextAsAudio(ctx, "please refund my order", ttsCfg, turnMeta, in, out)
	require.NoError(t, err)

	// streamTextAsAudio doesn't close inputChan; the test does so after
	// the helper returns to terminate the drain goroutine.
	close(in)
	<-drainDone

	// TTS service was invoked with the supplied text.
	assert.Equal(t, "please refund my order", fake.lastText)

	var userMsgElem *stage.StreamElement
	var audioChunkCount int
	for i := range collected {
		e := collected[i]
		switch {
		case e.Message != nil && e.Message.Role == "user":
			userMsgElem = &e
		case e.Audio != nil:
			audioChunkCount++
		}
	}

	require.NotNil(t, userMsgElem, "expected a user message element on the input channel")
	require.NotNil(t, userMsgElem.Meta.TurnID, "expected typed turn ID metadata")
	assert.NotEmpty(t, *userMsgElem.Meta.TurnID)

	msg := userMsgElem.Message
	require.NotNil(t, msg)
	assert.Equal(t, "user", msg.Role)
	assert.Equal(t, "please refund my order", msg.Content)
	require.Len(t, msg.Parts, 2, "expected text + audio parts")
	assert.Equal(t, types.ContentTypeText, msg.Parts[0].Type)
	require.NotNil(t, msg.Parts[0].Text)
	assert.Equal(t, "please refund my order", *msg.Parts[0].Text)
	assert.Equal(t, types.ContentTypeAudio, msg.Parts[1].Type)
	require.NotNil(t, msg.Parts[1].Media)
	// Audio is mirrored to a temp file as TTS chunks stream in; the user
	// message points at that file via FilePath rather than carrying the
	// bytes inline. The downstream MediaExternalizerStage copies the file
	// into media storage and replaces FilePath with a StorageReference.
	require.NotNil(t, msg.Parts[1].Media.FilePath)
	assert.NotEmpty(t, *msg.Parts[1].Media.FilePath, "audio payload should be referenced by FilePath")
	assert.Nil(t, msg.Parts[1].Media.Data, "audio payload must not be embedded inline anymore")

	// Caller-supplied metadata threaded through, plus turn_id always set.
	assert.Equal(t, "support-agent", msg.Meta["persona"])
	assert.Equal(t, true, msg.Meta["self_play"])
	assert.NotEmpty(t, msg.Meta["turn_id"])

	// At least one audio chunk should have been streamed (burst mode emits the
	// full payload as PCM chunks).
	assert.Greater(t, audioChunkCount, 0, "expected at least one audio chunk on the input channel")
}

func TestStreamTextAsAudio_RejectsEmptyText(t *testing.T) {
	reg, _ := newRegistryWithFakeTTS("fake", []byte{0x01, 0x02})
	de := &DuplexConversationExecutor{selfPlayRegistry: reg}

	in := make(chan stage.StreamElement, 4)
	out := make(chan stage.StreamElement)

	err := de.streamTextAsAudio(
		context.Background(),
		"",
		&config.TTSConfig{Provider: "fake", Voice: "v1"},
		nil,
		in,
		out,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is empty")
}

func TestStreamTextAsAudio_RejectsNilTTSConfig(t *testing.T) {
	reg, _ := newRegistryWithFakeTTS("fake", []byte{0x01, 0x02})
	de := &DuplexConversationExecutor{selfPlayRegistry: reg}

	in := make(chan stage.StreamElement, 4)
	out := make(chan stage.StreamElement)

	err := de.streamTextAsAudio(context.Background(), "hi", nil, nil, in, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ttsConfig is nil")
}

func TestStreamTextAsAudio_RejectsMissingRegistry(t *testing.T) {
	de := &DuplexConversationExecutor{} // no selfPlayRegistry

	in := make(chan stage.StreamElement, 4)
	out := make(chan stage.StreamElement)

	err := de.streamTextAsAudio(
		context.Background(),
		"hi",
		&config.TTSConfig{Provider: "fake", Voice: "v1"},
		nil,
		in,
		out,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-play registry not configured")
}

func TestResolveTTSProvider_ViaPersona(t *testing.T) {
	cfg := &config.Config{
		Voices: []config.VoiceBinding{{ID: "v1", Provider: "p1"}},
		LoadedTTSProviders: map[string]*config.Provider{
			"p1": {ID: "p1", Type: "cartesia", Voice: "vid", Capability: config.CapabilityTTS},
		},
	}
	persona := &config.UserPersonaPack{ID: "p", Voice: "v1"}
	got, err := resolveTTSProvider(cfg, persona)
	if err != nil {
		t.Fatalf("resolveTTSProvider: %v", err)
	}
	if got == nil || got.Voice != "vid" {
		t.Fatalf("got %+v, want voice=vid", got)
	}
}

func TestResolveTTSProvider_PersonaWithoutVoice(t *testing.T) {
	cfg := &config.Config{}
	persona := &config.UserPersonaPack{ID: "p"}
	got, err := resolveTTSProvider(cfg, persona)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil provider for persona without voice, got %+v", got)
	}
}

func TestTurnHasAudioPart(t *testing.T) {
	t.Run("no parts", func(t *testing.T) {
		assert.False(t, turnHasAudioPart(&config.TurnDefinition{}))
	})
	t.Run("text part only", func(t *testing.T) {
		turn := &config.TurnDefinition{Parts: []config.TurnContentPart{{Type: "text"}}}
		assert.False(t, turnHasAudioPart(turn))
	})
	t.Run("audio part present", func(t *testing.T) {
		turn := &config.TurnDefinition{Parts: []config.TurnContentPart{
			{Type: "text"},
			{Type: "audio"},
		}}
		assert.True(t, turnHasAudioPart(turn))
	})
}
