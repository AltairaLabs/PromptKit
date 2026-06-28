package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// scriptedVAD is a deterministic audio.VADAnalyzer for the continuous
// multi-turn integration test. It plays back a fixed state sequence (one state
// per Analyze call) so turn boundaries are exact rather than dependent on the
// real VAD's threshold behavior over synthetic audio. Reset clears only the
// current state — the index advances linearly across turns, so the script
// describes the whole session.
type scriptedVAD struct {
	mu     sync.Mutex
	states []audio.VADState
	idx    int
	cur    audio.VADState
}

func (v *scriptedVAD) Name() string { return "scripted-vad" }

func (v *scriptedVAD) Analyze(_ context.Context, _ []byte) (float64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.idx < len(v.states) {
		v.cur = v.states[v.idx]
		v.idx++
	} else {
		v.cur = audio.VADStateQuiet
	}
	return 0, nil
}

func (v *scriptedVAD) State() audio.VADState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cur
}

func (v *scriptedVAD) OnStateChange() <-chan audio.VADEvent { return nil }

func (v *scriptedVAD) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cur = audio.VADStateQuiet
}

// countRoles returns how many messages carry each of the user / assistant roles.
func countRoles(msgs []types.Message) (users, assistants int) {
	for i := range msgs {
		switch string(msgs[i].Role) {
		case "user":
			users++
		case "assistant":
			assistants++
		}
	}
	return users, assistants
}

// newDuplexTestExecutorMultiTurn builds the composed-VAD executor with a
// scripted VAD injected (req.vadOverride) and a short silence threshold, so the
// AudioTurn stage produces exact turn boundaries. The text mock provider returns
// a reply on every Predict call, so each fired turn yields an assistant message.
func newDuplexTestExecutorMultiTurn(
	t *testing.T, vad *scriptedVAD,
) (*DuplexConversationExecutor, *ConversationRequest, *statestore.ArenaStateStore) {
	t.Helper()

	textProvider := mock.NewProvider("test-mock-multiturn", "mock-model", false)

	reg := selfplay.NewRegistry(nil, nil, nil, nil)
	reg.GetTTSRegistry().Register(selfplay.TTSProviderMock, selfplay.NewMockTTS())
	reg.GetSTTRegistry().Register("fake-stt-mt", &fakeSTTService{transcript: "hello there"})

	executor := NewDuplexConversationExecutor(reg, nil, nil, nil, nil)
	store := statestore.NewArenaStateStore()

	sttProvider := &config.Provider{ID: "fake-stt-mt", Type: "fake-stt-mt", Role: config.RoleSTT}
	ttsProvider := &config.Provider{ID: "mock-tts-mt", Type: selfplay.TTSProviderMock, Role: config.RoleTTS}

	scenario := &config.Scenario{
		ID: "interactive-voice-multiturn-test",
		Duplex: &config.DuplexConfig{
			Timeout: "10s",
			TurnDetection: &config.TurnDetectionConfig{
				Mode: config.TurnDetectionModeVAD,
				VAD: &config.VADConfig{
					SilenceThresholdMs: 20, // short so turns complete quickly + deterministically
					MinSpeechMs:        5,
				},
			},
		},
		Turns: []config.TurnDefinition{},
	}

	cfg := &config.Config{
		LoadedProviders:    map[string]*config.Provider{},
		LoadedTTSProviders: map[string]*config.Provider{"mock-tts-mt": ttsProvider},
		Voices:             []config.VoiceBinding{{ID: "mock-tts-mt", Provider: "mock-tts-mt"}},
	}

	req := &ConversationRequest{
		Provider:         textProvider,
		Scenario:         scenario,
		Config:           cfg,
		RunID:            "test-run-multiturn",
		ConversationID:   "test-conv-multiturn",
		VoiceSTT:         sttProvider,
		VoiceOutputVoice: "mock-tts-mt",
		StateStoreConfig: &StateStoreConfig{Store: store},
		vadOverride:      vad,
	}

	return executor, req, store
}

// TestRunInteractiveVoice_VAD_ContinuousMultiTurn drives speech → silence →
// speech → silence through the REAL composed-VAD pipeline (scripted VAD, mock
// STT/LLM/TTS, no live keys) and asserts a reply PER turn: two user transcripts
// and two assistant messages materialize via the save stage. This exercises the
// continuous multi-turn behavior delivered by epic #1469 — the streaming
// provider firing once per EndOfTurn rather than batching every turn until the
// mic closes (the bug from PR #1457).
func TestRunInteractiveVoice_VAD_ContinuousMultiTurn(t *testing.T) {
	// Two turns, each: 2 speech frames then 2 silence frames. The first silence
	// frame sets the silence timer; after the silence threshold elapses the
	// second silence frame completes the turn. Reset() clears only the current
	// state, so the index runs linearly across both turns.
	vad := &scriptedVAD{states: []audio.VADState{
		audio.VADStateSpeaking, audio.VADStateSpeaking, audio.VADStateQuiet, audio.VADStateQuiet, // turn 1
		audio.VADStateSpeaking, audio.VADStateSpeaking, audio.VADStateQuiet, audio.VADStateQuiet, // turn 2
	}}

	exec, req, store := newDuplexTestExecutorMultiTurn(t, vad)

	mic := make(chan []byte)
	var played [][]byte
	var playMu sync.Mutex
	play := func(b []byte) {
		playMu.Lock()
		played = append(played, b)
		playMu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- exec.RunInteractiveVoice(ctx, req, mic, play, func() {}) }()

	feedTurn := func() {
		mic <- speechFrame()
		mic <- speechFrame()
		mic <- make([]byte, 320) // silence 1 — silence timer starts
		time.Sleep(40 * time.Millisecond)
		mic <- make([]byte, 320) // silence 2 — turn completes (silence ≥ 20ms)
		time.Sleep(40 * time.Millisecond)
	}

	feedTurn() // turn 1
	feedTurn() // turn 2
	close(mic)

	if err := <-runErr; err != nil {
		t.Fatalf("RunInteractiveVoice: %v", err)
	}

	state, err := store.Load(ctx, req.ConversationID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	users, assistants := countRoles(state.Messages)
	if users != 2 {
		t.Errorf("expected 2 user transcripts (one per turn), got %d (messages: %+v)", users, state.Messages)
	}
	if assistants != 2 {
		t.Errorf("expected 2 assistant replies (one per turn), got %d (messages: %+v)", assistants, state.Messages)
	}

	playMu.Lock()
	playCount := len(played)
	playMu.Unlock()
	if playCount == 0 {
		t.Error("expected playback audio from the TTS stage across the conversation, got none")
	}
}
