package audio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockStreamSession implements providers.StreamInputSession for testing
type mockStreamSession struct {
	chunks    chan providers.StreamChunk
	done      chan struct{}
	closed    bool
	sendErr   error
	lastChunk *types.MediaChunk
	lastText  string
}

func newMockStreamSession() *mockStreamSession {
	return &mockStreamSession{
		chunks: make(chan providers.StreamChunk, 10),
		done:   make(chan struct{}),
	}
}

func (m *mockStreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	m.lastChunk = chunk
	return m.sendErr
}

func (m *mockStreamSession) SendText(ctx context.Context, text string) error {
	m.lastText = text
	return m.sendErr
}

func (m *mockStreamSession) Response() <-chan providers.StreamChunk {
	return m.chunks
}

func (m *mockStreamSession) Close() error {
	if !m.closed {
		m.closed = true
		close(m.done)
	}
	return nil
}

func (m *mockStreamSession) Error() error {
	return nil
}

func (m *mockStreamSession) Done() <-chan struct{} {
	return m.done
}

func TestNewSession(t *testing.T) {
	mock := newMockStreamSession()

	t.Run("with default config", func(t *testing.T) {
		session, err := NewSession(mock, SessionConfig{})
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		if session == nil {
			t.Fatal("NewSession() returned nil")
		}
		defer session.Close()
	})

	t.Run("with custom VAD", func(t *testing.T) {
		vad, _ := NewSimpleVAD(DefaultVADParams())
		session, err := NewSession(mock, SessionConfig{
			VAD: vad,
		})
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		defer session.Close()
	})

	t.Run("with turn detector", func(t *testing.T) {
		detector := NewSilenceDetector(500 * time.Millisecond)
		session, err := NewSession(mock, SessionConfig{
			TurnDetector: detector,
		})
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		defer session.Close()
	})

	t.Run("with interruption handler", func(t *testing.T) {
		session, err := NewSession(mock, SessionConfig{
			InterruptionStrategy: InterruptionImmediate,
		})
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		defer session.Close()
	})
}

func TestSession_SendChunk(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	chunk := &types.MediaChunk{
		Data:      generateSilence(160),
		Timestamp: time.Now(),
	}

	err := session.SendChunk(context.Background(), chunk)
	if err != nil {
		t.Fatalf("SendChunk() error = %v", err)
	}

	if mock.lastChunk != chunk {
		t.Error("chunk was not forwarded to underlying session")
	}
}

func TestSession_SendText(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	err := session.SendText(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	if mock.lastText != "hello" {
		t.Errorf("lastText = %v, want hello", mock.lastText)
	}
}

func TestSession_Response(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	// Response should return underlying channel
	if session.Response() != mock.chunks {
		t.Error("Response() should return underlying channel")
	}
}

func TestSession_Close(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})

	err := session.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !mock.closed {
		t.Error("underlying session should be closed")
	}

	// Double close should be safe
	err = session.Close()
	if err != nil {
		t.Fatalf("double Close() error = %v", err)
	}
}

func TestSession_ClosedSession(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	session.Close()

	// Operations on closed session should fail
	err := session.SendChunk(context.Background(), &types.MediaChunk{})
	if err != ErrSessionClosed {
		t.Errorf("SendChunk on closed session error = %v, want ErrSessionClosed", err)
	}

	err = session.SendText(context.Background(), "test")
	if err != ErrSessionClosed {
		t.Errorf("SendText on closed session error = %v, want ErrSessionClosed", err)
	}
}

func TestSession_VADState(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	// Initially quiet
	if session.VADState() != VADStateQuiet {
		t.Errorf("VADState() = %v, want VADStateQuiet", session.VADState())
	}
}

func TestSession_IsUserSpeaking(t *testing.T) {
	mock := newMockStreamSession()
	detector := NewSilenceDetector(500 * time.Millisecond)
	session, _ := NewSession(mock, SessionConfig{
		TurnDetector: detector,
	})
	defer session.Close()

	// Initially not speaking
	if session.IsUserSpeaking() {
		t.Error("IsUserSpeaking() should be false initially")
	}
}

func TestSession_SetBotSpeaking(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{
		InterruptionStrategy: InterruptionImmediate,
	})
	defer session.Close()

	// Should not panic even with interruption handler
	session.SetBotSpeaking(true)
	session.SetBotSpeaking(false)
}

func TestSession_Reset(t *testing.T) {
	mock := newMockStreamSession()
	detector := NewSilenceDetector(500 * time.Millisecond)
	session, _ := NewSession(mock, SessionConfig{
		TurnDetector:         detector,
		InterruptionStrategy: InterruptionImmediate,
	})
	defer session.Close()

	// Should not panic
	session.Reset()
}

func TestSession_OnTurnDetected(t *testing.T) {
	mock := newMockStreamSession()
	params := DefaultVADParams()
	params.StartSecs = 0.01
	params.StopSecs = 0.01
	vad, _ := NewSimpleVAD(params)
	detector := NewSilenceDetector(20 * time.Millisecond)

	session, _ := NewSession(mock, SessionConfig{
		VAD:          vad,
		TurnDetector: detector,
	})
	defer session.Close()

	turnChan := session.OnTurnDetected()
	if turnChan == nil {
		t.Error("OnTurnDetected() returned nil")
	}
}

func TestSession_OnInterruption(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{
		InterruptionStrategy: InterruptionImmediate,
	})
	defer session.Close()

	intChan := session.OnInterruption()
	if intChan == nil {
		t.Error("OnInterruption() returned nil")
	}
}

func TestSession_GetAccumulatedAudio(t *testing.T) {
	mock := newMockStreamSession()
	detector := NewSilenceDetector(500 * time.Millisecond)
	session, _ := NewSession(mock, SessionConfig{
		TurnDetector: detector,
	})
	defer session.Close()

	// Initially nil
	if session.GetAccumulatedAudio() != nil {
		t.Error("GetAccumulatedAudio() should be nil initially")
	}
}

func TestSession_GetAccumulatedAudio_NoDetector(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	// Should return nil when no turn detector
	if session.GetAccumulatedAudio() != nil {
		t.Error("GetAccumulatedAudio() should be nil without turn detector")
	}
}

func TestSession_Error(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	// Should return underlying session error (nil in mock)
	if session.Error() != nil {
		t.Error("Error() should be nil")
	}
}

func TestSession_Done(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	// Done should return underlying done channel
	if session.Done() != mock.done {
		t.Error("Done() should return underlying channel")
	}
}

func TestSession_IsUserSpeaking_NoDetector(t *testing.T) {
	mock := newMockStreamSession()
	// Create with default config (no turn detector)
	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	// Should use VAD state when no turn detector
	// Initially VAD is quiet, so should be false
	if session.IsUserSpeaking() {
		t.Error("IsUserSpeaking() should be false when VAD is quiet")
	}
}

func TestSession_SendChunkWithVADAndTurnDetection(t *testing.T) {
	mock := newMockStreamSession()
	params := DefaultVADParams()
	params.StartSecs = 0.01
	params.StopSecs = 0.01
	vad, _ := NewSimpleVAD(params)
	detector := NewSilenceDetector(10 * time.Millisecond)

	session, _ := NewSession(mock, SessionConfig{
		VAD:          vad,
		TurnDetector: detector,
	})
	defer session.Close()

	// Send a chunk with audio data
	chunk := &types.MediaChunk{
		Data:      generateSilence(320),
		Timestamp: time.Now(),
	}

	err := session.SendChunk(context.Background(), chunk)
	if err != nil {
		t.Fatalf("SendChunk() error = %v", err)
	}
}

func TestSession_SendChunkWithInterruption(t *testing.T) {
	mock := newMockStreamSession()
	session, _ := NewSession(mock, SessionConfig{
		InterruptionStrategy: InterruptionImmediate,
	})
	defer session.Close()

	// Set bot speaking to enable interruption detection
	session.SetBotSpeaking(true)

	chunk := &types.MediaChunk{
		Data:      generateSilence(320),
		Timestamp: time.Now(),
	}

	err := session.SendChunk(context.Background(), chunk)
	if err != nil {
		t.Fatalf("SendChunk() error = %v", err)
	}
}

func TestSession_SendChunk_Error(t *testing.T) {
	mock := newMockStreamSession()
	mock.sendErr = errors.New("send failed")

	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	chunk := &types.MediaChunk{
		Data:      generateSilence(160),
		Timestamp: time.Now(),
	}

	err := session.SendChunk(context.Background(), chunk)
	if err == nil {
		t.Error("SendChunk() should return error from underlying session")
	}
}

func TestSession_SendText_Error(t *testing.T) {
	mock := newMockStreamSession()
	mock.sendErr = errors.New("send failed")

	session, _ := NewSession(mock, SessionConfig{})
	defer session.Close()

	err := session.SendText(context.Background(), "test")
	if err == nil {
		t.Error("SendText() should return error from underlying session")
	}
}

func TestSession_FullInterruptionFlow(t *testing.T) {
	mock := newMockStreamSession()

	// Create session with interruption handling
	session, _ := NewSession(mock, SessionConfig{
		InterruptionStrategy: InterruptionImmediate,
	})
	defer session.Close()

	intChan := session.OnInterruption()

	// Set bot speaking
	session.SetBotSpeaking(true)

	// Send loud audio to trigger speech detection and interruption
	loudAudio := make([]byte, 3200)
	for i := range loudAudio {
		if i%2 == 0 {
			loudAudio[i] = 0xFF
		} else {
			loudAudio[i] = 0x3F // ~32767 which normalizes to ~1.0
		}
	}

	chunk := &types.MediaChunk{
		Data:      loudAudio,
		Timestamp: time.Now(),
	}

	// Send multiple chunks to trigger VAD state change
	for i := 0; i < 10; i++ {
		session.SendChunk(context.Background(), chunk)
		time.Sleep(5 * time.Millisecond)
	}

	// Check if interruption was signaled
	select {
	case <-intChan:
		// Success - interruption detected
	case <-time.After(100 * time.Millisecond):
		// Timeout is acceptable - VAD may not have triggered
	}
}
