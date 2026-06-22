package engine

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
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

// TestRunInteractiveVoice_NonStreamingProvider_ReturnsNotImplemented verifies
// that RunInteractiveVoice returns an error for providers that do not implement
// StreamInputSupport (the VAD path, implemented in Task 7).
func TestRunInteractiveVoice_NonStreamingProvider_ReturnsNotImplemented(t *testing.T) {
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

func init() {
	// Verify ResponseRepository interface compliance at compile time.
	var _ mock.ResponseRepository = (*interactiveVoiceTestRepo)(nil)
	var _ providers.Provider = (*mockNonStreamingProvider)(nil) // already confirmed in duplex_conversation_executor_test.go
}
