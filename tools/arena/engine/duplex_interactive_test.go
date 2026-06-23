package engine

import (
	"context"
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// interactiveVoiceTestRepo is a minimal ResponseRepository for TestRunInteractiveVoice_ASM
// that returns an audio fixture on every GetTurn call. It implements just enough of the
// mock.ResponseRepository interface to exercise the audio playback path.
type interactiveVoiceTestRepo struct {
	// audioFile is an absolute path to a raw PCM16 mono file. The mock session's
	// emitAudioChunks reads this file and sends it as MediaData chunks, which the
	// DuplexProviderStage converts into elem.Audio on the output channel.
	audioFile   string
	sampleRate  int
	mimeType    string
	textContent string
}

func (r *interactiveVoiceTestRepo) GetResponse(ctx context.Context, params mock.ResponseParams) (string, error) {
	turn, err := r.GetTurn(ctx, params)
	if err != nil || turn == nil {
		return r.textContent, err
	}
	return turn.Content, nil
}

func (r *interactiveVoiceTestRepo) GetTurn(_ context.Context, _ mock.ResponseParams) (*mock.Turn, error) {
	return &mock.Turn{
		Type:            "audio",
		Content:         r.textContent,
		AudioFile:       r.audioFile,
		AudioSampleRate: r.sampleRate,
		AudioMIMEType:   r.mimeType,
	}, nil
}

// newDuplexTestExecutorASM builds a DuplexConversationExecutor backed by a mock
// StreamInputSupport provider and a minimal ConversationRequest configured for
// ASM mode (no client-side VAD stage). The scenario carries no turns because
// RunInteractiveVoice replaces the turn loop entirely.
//
// The provider is wired with a file-backed ResponseRepository so that every
// auto-respond call emits PCM16 audio MediaData chunks. The audio fixture used
// is testdata/test.pcm (raw s16le mono, 16 kHz). Using a deterministic file
// eliminates the goroutine-polling race that arises when trying to inject
// session.WithResponseChunks after session creation.
//
// Returns the executor, the request, and the mock provider (for introspection).
func newDuplexTestExecutorASM(t *testing.T) (*DuplexConversationExecutor, *ConversationRequest, *mock.StreamingProvider) {
	t.Helper()

	// testdata/test.pcm is raw s16le mono PCM at the module's default 16 kHz.
	// It is used by other duplex tests in this package (e.g. TestDuplexStateStore_*)
	// so it is guaranteed to exist.
	audioFile := "testdata/test.pcm"

	repo := &interactiveVoiceTestRepo{
		audioFile:   audioFile,
		sampleRate:  16000,
		mimeType:    "audio/pcm",
		textContent: "ok",
	}

	// Use scenario ID matching the ConversationRequest so applyMockScenarioContext
	// threads it into the session metadata and resolveTurn can look it up.
	const scenarioID = "interactive-voice-test"

	mockProvider := mock.NewStreamingProvider("test-mock-asm", "mock-model", false)
	// WithAutoRespond causes EndInput() → emitAutoResponse() to fire when the
	// pipeline signals end-of-user-speech (EndOfStream element).
	mockProvider.WithAutoRespond("ok")
	// Wire the audio-fixture repository so every session's auto-respond emits PCM16
	// MediaData chunks (via emitAudioChunks) in addition to text. The scenario ID
	// must match what applyMockScenarioContext threads into SessionConfig.Metadata.
	mockProvider.WithMockResponses(repo, scenarioID, "")
	// WithCloseAfterTurns(1) is propagated into sessions at CreateStreamSession time
	// (before any audio arrives), so forwardResponseElements exits cleanly after the
	// first turn completes rather than waiting for the 30-second finalResponseTimeout
	// or a context cancellation. This keeps the test well under the 5-second limit.
	mockProvider.WithCloseAfterTurns(1)

	executor := NewDuplexConversationExecutor(nil, nil, nil, nil, nil)

	store := statestore.NewArenaStateStore()
	scenario := &config.Scenario{
		ID: scenarioID,
		Duplex: &config.DuplexConfig{
			Timeout: "10s",
			TurnDetection: &config.TurnDetectionConfig{
				// ASM: provider-native turn detection; no client-side VAD stage
				// is added to the pipeline, keeping the test simple.
				Mode: config.TurnDetectionModeASM,
			},
		},
		// No turns — RunInteractiveVoice owns the session lifecycle.
		Turns: []config.TurnDefinition{},
	}

	req := &ConversationRequest{
		Provider:       mockProvider,
		Scenario:       scenario,
		Config:         &config.Config{LoadedProviders: map[string]*config.Provider{}},
		RunID:          "test-run-voice",
		ConversationID: "test-conv-voice",
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	return executor, req, mockProvider
}

// TestRunInteractiveVoice_ASM_EchoesAudioToPlayback verifies that:
//   - A mic frame pushed through RunInteractiveVoice reaches the pipeline.
//   - The mock provider's audio MediaData response produces at least one play call.
//   - Closing mic causes the session to end cleanly (no error returned).
//
// The provider is configured with WithCloseAfterTurns(1) (set at provider level,
// propagated to the session at CreateStreamSession time) so that forwardResponseElements
// exits after the first response without waiting for the 30-second finalResponseTimeout.
// This keeps the test well within the 2-second budget.
func TestRunInteractiveVoice_ASM_EchoesAudioToPlayback(t *testing.T) {
	exec, req, _ := newDuplexTestExecutorASM(t)

	mic := make(chan []byte, 4)
	var played [][]byte
	play := func(b []byte) { played = append(played, b) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mic <- make([]byte, 320) // one 10ms-ish PCM16 frame @16kHz
	close(mic)

	if err := exec.RunInteractiveVoice(ctx, req, mic, play); err != nil {
		t.Fatalf("RunInteractiveVoice: %v", err)
	}
	if len(played) == 0 {
		t.Fatal("expected playback frames from the mock streaming provider, got none")
	}
}

// TestRunInteractiveVoice_VAD_RequiresSTTProvider verifies that RunInteractiveVoice
// returns an error when a non-StreamInputSupport provider is used without an STT
// provider configured (the VAD path requires --voice-stt to be set).
func TestRunInteractiveVoice_VAD_RequiresSTTProvider(t *testing.T) {
	exec := NewDuplexConversationExecutor(nil, nil, nil, nil, nil)

	req := &ConversationRequest{
		Provider: &mockNonStreamingProvider{},
		Scenario: &config.Scenario{
			ID: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "5s",
			},
		},
		Config:         &config.Config{LoadedProviders: map[string]*config.Provider{}},
		RunID:          "test",
		ConversationID: "test",
	}

	mic := make(chan []byte)
	close(mic)

	ctx := context.Background()
	err := exec.RunInteractiveVoice(ctx, req, mic, func(_ []byte) {})
	if err == nil {
		t.Fatal("expected error for non-streaming provider, got nil")
	}
}

// fakeSTTService is a minimal stt.Service for the VAD voice test. It returns a
// fixed transcript for any audio so the composed pipeline produces a
// deterministic user message. It satisfies both base.STTProvider (via Transcribe)
// and the extended stt.Service methods (TranscribeBytes, SupportedFormats).
type fakeSTTService struct {
	transcript string
}

func (f *fakeSTTService) Name() string                        { return "fake-stt" }
func (f *fakeSTTService) Type() base.ProviderType             { return base.ProviderTypeSTT }
func (f *fakeSTTService) Pricing() *base.PricingDescriptor    { return nil }
func (f *fakeSTTService) Validate() error                     { return nil }
func (f *fakeSTTService) Init(_ context.Context) error        { return nil }
func (f *fakeSTTService) HealthCheck(_ context.Context) error { return nil }
func (f *fakeSTTService) Close() error                        { return nil }

func (f *fakeSTTService) Transcribe(_ context.Context, _ base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: f.transcript}, nil
}

func (f *fakeSTTService) TranscribeBytes(
	_ context.Context, _ []byte, _ stt.TranscriptionConfig,
) (string, error) {
	return f.transcript, nil
}

func (f *fakeSTTService) SupportedFormats() []string { return []string{stt.FormatPCM} }

// hasRole reports whether any message in msgs has the given role.
func hasRole(msgs []types.Message, role string) bool {
	for i := range msgs {
		if string(msgs[i].Role) == role {
			return true
		}
	}
	return false
}

// speechFrame returns a 20ms PCM16 mono frame at 16 kHz carrying a sine tone.
// The non-zero waveform makes the AudioTurnStage's VAD register signal; even if
// VAD never flips to "speaking", the accumulated buffer exceeds the stage's
// min-audio threshold and is flushed as a turn when the mic channel closes.
func speechFrame() []byte {
	const samples = 320 // 20ms @ 16kHz
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/16000))
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	return buf
}

// newDuplexTestExecutorVAD builds a DuplexConversationExecutor wired for the
// composed VAD voice path: a plain text mock provider (NOT StreamInputSupport),
// a fake STT service resolved via the self-play STT registry, and a mock TTS
// service resolved via the TTS registry. The request carries VoiceSTT (so the
// STT branch is taken) and a TTS-role provider for VoiceOutputVoice resolution.
func newDuplexTestExecutorVAD(
	t *testing.T,
) (*DuplexConversationExecutor, *ConversationRequest, *statestore.ArenaStateStore) {
	t.Helper()

	// Plain text mock provider: implements providers.Provider (Predict path) but
	// not providers.StreamInputSupport, so RunInteractiveVoice routes to the VAD
	// composed pipeline.
	textProvider := mock.NewProvider("test-mock-text", "mock-model", false)
	if _, ok := providers.Provider(textProvider).(providers.StreamInputSupport); ok {
		t.Fatal("test setup error: mock text provider must NOT implement StreamInputSupport")
	}

	// Self-play registry provides the STT/TTS registries. Register fakes so the
	// VAD runner resolves services without API keys.
	reg := selfplay.NewRegistry(nil, nil, nil, nil)
	reg.GetTTSRegistry().Register(selfplay.TTSProviderMock, selfplay.NewMockTTS())
	reg.GetSTTRegistry().Register("fake-stt", &fakeSTTService{transcript: "hello there"})

	executor := NewDuplexConversationExecutor(reg, nil, nil, nil, nil)

	store := statestore.NewArenaStateStore()

	sttProvider := &config.Provider{
		ID:   "fake-stt",
		Type: "fake-stt",
		Role: config.RoleSTT,
	}
	ttsProvider := &config.Provider{
		ID:   "mock-tts",
		Type: selfplay.TTSProviderMock,
		Role: config.RoleTTS,
	}

	scenario := &config.Scenario{
		ID: "interactive-voice-vad-test",
		Duplex: &config.DuplexConfig{
			Timeout: "10s",
			TurnDetection: &config.TurnDetectionConfig{
				Mode: config.TurnDetectionModeVAD,
			},
		},
		Turns: []config.TurnDefinition{},
	}

	cfg := &config.Config{
		LoadedProviders:    map[string]*config.Provider{},
		LoadedTTSProviders: map[string]*config.Provider{"mock-tts": ttsProvider},
		Voices: []config.VoiceBinding{
			{ID: "mock-tts", Provider: "mock-tts"},
		},
	}

	req := &ConversationRequest{
		Provider:         textProvider,
		Scenario:         scenario,
		Config:           cfg,
		RunID:            "test-run-vad",
		ConversationID:   "test-conv-vad",
		VoiceSTT:         sttProvider,
		VoiceOutputVoice: "mock-tts",
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	return executor, req, store
}

// TestRunInteractiveVoice_VAD_MaterializesTurn verifies the composed VAD voice
// pipeline drives a plain text provider: mic audio is segmented into a turn,
// transcribed (fake STT), answered by the text LLM, synthesized back to audio
// (mock TTS), and the transcript + assistant reply are persisted to the state
// store — materializing history identically to a text run.
func TestRunInteractiveVoice_VAD_MaterializesTurn(t *testing.T) {
	exec, req, store := newDuplexTestExecutorVAD(t)

	mic := make(chan []byte, 64)
	var played [][]byte
	play := func(b []byte) { played = append(played, b) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Feed speech frames followed by trailing silence so the AudioTurnStage has a
	// full utterance buffered; closing the mic flushes the turn downstream.
	for i := 0; i < 20; i++ {
		mic <- speechFrame()
	}
	for i := 0; i < 20; i++ {
		mic <- make([]byte, 320) // silence
	}
	close(mic)

	if err := exec.RunInteractiveVoice(ctx, req, mic, play); err != nil {
		t.Fatalf("RunInteractiveVoice: %v", err)
	}

	state, err := store.Load(ctx, req.ConversationID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if !hasRole(state.Messages, "user") || !hasRole(state.Messages, "assistant") {
		t.Fatalf("expected user+assistant messages, got %+v", state.Messages)
	}
	if len(played) == 0 {
		t.Fatal("expected playback audio from the TTS stage, got none")
	}
}

// recordingTTS embeds *selfplay.MockTTSService (which satisfies both base.TTSProvider
// and tts.Service) and overrides Synthesize to capture every text the pipeline
// sends for synthesis. Thread-safe: the pipeline goroutine and the test goroutine
// can both access inputs without a data race.
type recordingTTS struct {
	*selfplay.MockTTSService
	mu     sync.Mutex
	inputs []string
}

func (r *recordingTTS) Synthesize(
	ctx context.Context, text string, cfg tts.SynthesisConfig,
) (io.ReadCloser, error) {
	r.mu.Lock()
	r.inputs = append(r.inputs, text)
	r.mu.Unlock()
	return r.MockTTSService.Synthesize(ctx, text, cfg)
}

// Inputs returns a snapshot of the texts sent to Synthesize so far.
func (r *recordingTTS) Inputs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(r.inputs))
	copy(cp, r.inputs)
	return cp
}

// newDuplexTestExecutorVADWithRecordingTTS mirrors newDuplexTestExecutorVAD but
// wraps the mock TTS in a recordingTTS so the test can assert which texts were
// sent to synthesis.
func newDuplexTestExecutorVADWithRecordingTTS(
	t *testing.T,
) (*DuplexConversationExecutor, *ConversationRequest, *statestore.ArenaStateStore, *recordingTTS) {
	t.Helper()

	textProvider := mock.NewProvider("test-mock-text-rec", "mock-model", false)
	if _, ok := providers.Provider(textProvider).(providers.StreamInputSupport); ok {
		t.Fatal("test setup error: mock text provider must NOT implement StreamInputSupport")
	}

	reg := selfplay.NewRegistry(nil, nil, nil, nil)
	underlying := selfplay.NewMockTTS()
	recorder := &recordingTTS{MockTTSService: underlying}
	reg.GetTTSRegistry().Register(selfplay.TTSProviderMock, recorder)
	reg.GetSTTRegistry().Register("fake-stt-rec", &fakeSTTService{transcript: "hello there"})

	executor := NewDuplexConversationExecutor(reg, nil, nil, nil, nil)

	store := statestore.NewArenaStateStore()

	sttProvider := &config.Provider{
		ID:   "fake-stt-rec",
		Type: "fake-stt-rec",
		Role: config.RoleSTT,
	}
	ttsProvider := &config.Provider{
		ID:   "mock-tts-rec",
		Type: selfplay.TTSProviderMock,
		Role: config.RoleTTS,
	}

	scenario := &config.Scenario{
		ID: "interactive-voice-vad-rec-test",
		Duplex: &config.DuplexConfig{
			Timeout: "10s",
			TurnDetection: &config.TurnDetectionConfig{
				Mode: config.TurnDetectionModeVAD,
			},
		},
		Turns: []config.TurnDefinition{},
	}

	cfg := &config.Config{
		LoadedProviders:    map[string]*config.Provider{},
		LoadedTTSProviders: map[string]*config.Provider{"mock-tts-rec": ttsProvider},
		Voices: []config.VoiceBinding{
			{ID: "mock-tts-rec", Provider: "mock-tts-rec"},
		},
	}

	req := &ConversationRequest{
		Provider:         textProvider,
		Scenario:         scenario,
		Config:           cfg,
		RunID:            "test-run-vad-rec",
		ConversationID:   "test-conv-vad-rec",
		VoiceSTT:         sttProvider,
		VoiceOutputVoice: "mock-tts-rec",
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	return executor, req, store, recorder
}

// TestRunInteractiveVoice_VAD_AssistantOnlySynthesized asserts that the TTS
// stage receives only the assistant's reply, never the user's own transcript.
// Without the AssistantTTSFilterStage between the save stage and the TTS stage,
// the user transcript (wrapped as a user Message by STTUserMessageStage) passes
// through unchanged and the pipeline speaks the user's words back at them.
// The filter drops any Message element whose Role is not "assistant".
func TestRunInteractiveVoice_VAD_AssistantOnlySynthesized(t *testing.T) {
	exec, req, _, recorder := newDuplexTestExecutorVADWithRecordingTTS(t)

	mic := make(chan []byte, 64)
	var played [][]byte
	play := func(b []byte) { played = append(played, b) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 20; i++ {
		mic <- speechFrame()
	}
	for i := 0; i < 20; i++ {
		mic <- make([]byte, 320) // silence to flush the VAD turn
	}
	close(mic)

	if err := exec.RunInteractiveVoice(ctx, req, mic, play); err != nil {
		t.Fatalf("RunInteractiveVoice: %v", err)
	}

	inputs := recorder.Inputs()
	if len(inputs) == 0 {
		t.Fatal("expected at least one TTS synthesis call, got none")
	}

	const userTranscript = "hello there"
	for _, s := range inputs {
		if s == userTranscript {
			t.Errorf("TTS received the user transcript %q — only assistant messages should be synthesized", s)
		}
	}

	if len(played) == 0 {
		t.Fatal("expected playback audio from the TTS stage, got none")
	}
}

// chunkInjectingStreamingProvider wraps *mock.StreamingProvider and injects
// custom responseChunks into each session at CreateStreamSession time. This
// lets tests configure exact StreamChunk sequences (including metadata like
// input_transcription) without modifying the runtime mock package.
type chunkInjectingStreamingProvider struct {
	*mock.StreamingProvider
	chunks []providers.StreamChunk
}

// CreateStreamSession delegates to the embedded provider and then injects
// the configured response chunks into the created session.
func (p *chunkInjectingStreamingProvider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	sess, err := p.StreamingProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if mockSess, ok := sess.(*mock.MockStreamSession); ok {
		mockSess.WithResponseChunks(p.chunks)
	}
	return sess, nil
}

// newDuplexTestExecutorASMWithTranscript builds a DuplexConversationExecutor
// whose mock ASM session emits an input_transcription chunk followed by an
// assistant turn. Used by TestRunInteractiveVoice_ASM_MaterializesUserTranscript
// to verify that the interactive voice path creates a user message and populates
// it with the transcription text.
func newDuplexTestExecutorASMWithTranscript(
	t *testing.T,
) (*DuplexConversationExecutor, *ConversationRequest, *statestore.ArenaStateStore) {
	t.Helper()

	const scenarioID = "interactive-voice-transcript-test"
	finishReason := "stop"
	chunks := []providers.StreamChunk{
		// input_transcription chunk — what the user said (mirrors Gemini Live API format)
		{
			Metadata: map[string]interface{}{
				"type":          "input_transcription",
				"transcription": "hello there from the user",
			},
		},
		// assistant text chunk with FinishReason to close the turn
		{
			Content:      "hi, how can I help?",
			Delta:        "hi, how can I help?",
			FinishReason: &finishReason,
		},
	}

	inner := mock.NewStreamingProvider("test-mock-transcript", "mock-model", false)
	inner.WithAutoRespond("ok")
	// Close the session after one turn so forwardResponseElements exits cleanly
	// without waiting for the 30-second finalResponseTimeout.
	inner.WithCloseAfterTurns(1)

	mockProvider := &chunkInjectingStreamingProvider{
		StreamingProvider: inner,
		chunks:            chunks,
	}

	executor := NewDuplexConversationExecutor(nil, nil, nil, nil, nil)
	store := statestore.NewArenaStateStore()

	scenario := &config.Scenario{
		ID: scenarioID,
		Duplex: &config.DuplexConfig{
			Timeout: "10s",
			TurnDetection: &config.TurnDetectionConfig{
				Mode: config.TurnDetectionModeASM,
			},
		},
		Turns: []config.TurnDefinition{},
	}

	req := &ConversationRequest{
		Provider:       mockProvider,
		Scenario:       scenario,
		Config:         &config.Config{LoadedProviders: map[string]*config.Provider{}},
		RunID:          "test-run-transcript",
		ConversationID: "test-conv-transcript",
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	return executor, req, store
}

// TestRunInteractiveVoice_ASM_MaterializesUserTranscript verifies that when
// the provider emits an input_transcription event, the interactive ASM voice
// path creates a user message in the state store whose Content is set to the
// transcript text (and that an assistant message is also persisted).
//
// RED phase: before the fix, no user message exists in the state store
// (or the message has empty Content) because feedMicToPipeline never emits
// a user Message element with a turn_id — so DuplexProviderStage silently
// discards the transcription (gate: EndOfStream && turnID != "").
func TestRunInteractiveVoice_ASM_MaterializesUserTranscript(t *testing.T) {
	exec, req, store := newDuplexTestExecutorASMWithTranscript(t)

	mic := make(chan []byte, 4)
	play := func(_ []byte) {}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mic <- make([]byte, 320) // one mic frame
	close(mic)

	if err := exec.RunInteractiveVoice(ctx, req, mic, play); err != nil {
		t.Fatalf("RunInteractiveVoice: %v", err)
	}

	state, err := store.Load(ctx, req.ConversationID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}

	const wantTranscript = "hello there from the user"
	const wantAssistant = "hi, how can I help?"

	var foundUser, foundAssistant bool
	for i := range state.Messages {
		msg := &state.Messages[i]
		switch string(msg.Role) {
		case "user":
			foundUser = true
			if msg.Content != wantTranscript {
				t.Errorf("user message Content = %q, want %q", msg.Content, wantTranscript)
			}
		case "assistant":
			foundAssistant = true
			if msg.Content != wantAssistant {
				t.Errorf("assistant message Content = %q, want %q", msg.Content, wantAssistant)
			}
		}
	}

	if !foundUser {
		t.Errorf("no user message in state store; got messages: %+v", state.Messages)
	}
	if !foundAssistant {
		t.Errorf("no assistant message in state store; got messages: %+v", state.Messages)
	}
}

// newDuplexTestExecutorVADWithStreamingProvider builds a DuplexConversationExecutor
// whose LLM provider implements StreamInputSupport (like the OpenAI provider does
// unconditionally) but has VoiceSTT set in the request. This is the exact scenario
// that triggered the routing bug: the StreamInputSupport type assertion returned true
// even for plain text models, routing to ASM and producing a model_not_found error.
//
// The helper mirrors newDuplexTestExecutorVAD but substitutes a StreamingProvider
// for the text provider while still setting req.VoiceSTT. The streaming provider's
// sessions list gives us a zero-cost observable to verify CreateStreamSession was
// never called (ASM path not taken).
func newDuplexTestExecutorVADWithStreamingProvider(
	t *testing.T,
) (*DuplexConversationExecutor, *ConversationRequest, *statestore.ArenaStateStore, *mock.StreamingProvider) {
	t.Helper()

	// StreamingProvider implements StreamInputSupport — this is the crux of the
	// routing bug test. If VoiceSTT is set but the type assertion fires first,
	// CreateStreamSession gets called (wrong). The fix must route to VAD first.
	streamingProvider := mock.NewStreamingProvider("test-mock-streaming-vad", "mock-model", false)
	// Configure auto-respond so the plain text Predict path (used by the VAD
	// composed pipeline's ProviderStage) returns a deterministic assistant reply.
	streamingProvider.WithAutoRespond("hello")

	reg := selfplay.NewRegistry(nil, nil, nil, nil)
	reg.GetTTSRegistry().Register(selfplay.TTSProviderMock, selfplay.NewMockTTS())
	reg.GetSTTRegistry().Register("fake-stt-routing", &fakeSTTService{transcript: "route me to vad"})

	executor := NewDuplexConversationExecutor(reg, nil, nil, nil, nil)

	store := statestore.NewArenaStateStore()

	sttProvider := &config.Provider{
		ID:   "fake-stt-routing",
		Type: "fake-stt-routing",
		Role: config.RoleSTT,
	}
	ttsProvider := &config.Provider{
		ID:   "mock-tts-routing",
		Type: selfplay.TTSProviderMock,
		Role: config.RoleTTS,
	}

	scenario := &config.Scenario{
		ID: "interactive-voice-routing-test",
		Duplex: &config.DuplexConfig{
			Timeout: "10s",
			TurnDetection: &config.TurnDetectionConfig{
				Mode: config.TurnDetectionModeVAD,
			},
		},
		Turns: []config.TurnDefinition{},
	}

	cfg := &config.Config{
		LoadedProviders:    map[string]*config.Provider{},
		LoadedTTSProviders: map[string]*config.Provider{"mock-tts-routing": ttsProvider},
		Voices: []config.VoiceBinding{
			{ID: "mock-tts-routing", Provider: "mock-tts-routing"},
		},
	}

	req := &ConversationRequest{
		Provider:         streamingProvider, // implements StreamInputSupport
		Scenario:         scenario,
		Config:           cfg,
		RunID:            "test-run-vad-routing",
		ConversationID:   "test-conv-vad-routing",
		VoiceSTT:         sttProvider, // explicit composed-path signal
		VoiceOutputVoice: "mock-tts-routing",
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	return executor, req, store, streamingProvider
}

// TestRunInteractiveVoice_RoutesToVADWhenVoiceSTTSet asserts that when req.VoiceSTT
// is non-nil and the provider also implements StreamInputSupport, RunInteractiveVoice
// takes the composed VAD path — NOT the ASM/realtime path.
//
// This is the bug regression test: before the fix the StreamInputSupport type
// assertion fired first, CreateStreamSession was called, and the realtime session
// attempted to start gpt-4o-realtime-preview → model_not_found. After the fix
// the VoiceSTT guard routes to VAD before the type assertion runs.
//
// Assertions:
//  1. streamingProvider.GetSessions() is empty — CreateStreamSession was never
//     called, confirming the ASM branch was not entered.
//  2. A user + assistant turn materialized in the state store — the VAD composed
//     pipeline ran end-to-end (STT → text LLM → TTS → save).
//  3. RunInteractiveVoice returned nil — the run completed without error.
func TestRunInteractiveVoice_RoutesToVADWhenVoiceSTTSet(t *testing.T) {
	exec, req, store, streamingProvider := newDuplexTestExecutorVADWithStreamingProvider(t)

	// Sanity check: the streaming provider DOES implement StreamInputSupport —
	// without the fix this would cause the ASM branch to be taken.
	if _, ok := req.Provider.(providers.StreamInputSupport); !ok {
		t.Fatal("test setup error: provider must implement StreamInputSupport for this test to be meaningful")
	}

	mic := make(chan []byte, 64)
	var played [][]byte
	play := func(b []byte) { played = append(played, b) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 20; i++ {
		mic <- speechFrame()
	}
	for i := 0; i < 20; i++ {
		mic <- make([]byte, 320) // silence to flush the VAD turn
	}
	close(mic)

	if err := exec.RunInteractiveVoice(ctx, req, mic, play); err != nil {
		t.Fatalf("RunInteractiveVoice: %v", err)
	}

	// Assertion 1: ASM session must NOT have been created.
	if sessions := streamingProvider.GetSessions(); len(sessions) != 0 {
		t.Errorf(
			"expected CreateStreamSession to not be called (VAD path should have been taken), "+
				"but %d ASM session(s) were created",
			len(sessions),
		)
	}

	// Assertion 2: VAD pipeline persisted history (user + assistant turn).
	state, err := store.Load(ctx, req.ConversationID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if !hasRole(state.Messages, "user") || !hasRole(state.Messages, "assistant") {
		t.Fatalf("expected user+assistant messages from the VAD pipeline, got %+v", state.Messages)
	}

	// Assertion 3: TTS produced audio.
	if len(played) == 0 {
		t.Fatal("expected playback audio from the TTS stage (VAD path), got none")
	}
}

func init() {
	// Verify ResponseRepository interface compliance at compile time.
	var _ mock.ResponseRepository = (*interactiveVoiceTestRepo)(nil)
	var _ providers.Provider = (*mockNonStreamingProvider)(nil) // already confirmed in duplex_conversation_executor_test.go
	// Verify the fake STT satisfies the full stt.Service interface used by the VAD pipeline.
	var _ stt.Service = (*fakeSTTService)(nil)
	// Confirm the mock TTS satisfies tts.Service (the TTS stage's contract).
	var _ tts.Service = (*selfplay.MockTTSService)(nil)
	// Verify chunkInjectingStreamingProvider implements StreamInputSupport.
	var _ providers.StreamInputSupport = (*chunkInjectingStreamingProvider)(nil)
}
