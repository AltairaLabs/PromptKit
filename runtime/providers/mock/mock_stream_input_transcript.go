package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A realtime provider transcribes the caller's speech and sends the
// transcript on the response stream, where the duplex pipeline runs input
// guardrails on it. A spoken turn whose mock response scripts an
// input_transcript gets that transcript ahead of its reply, in the shape
// OpenAI Realtime sends a completed input transcription, naming the turn's
// input so a blocked turn can be forgotten (providers.TurnForgetter). The
// mock records what it is asked to forget and the notes it is given
// (providers.ContextNoter); neither changes what it replies.

// inputTranscriptionChunk is the final transcript of the caller's input on
// turnNumber, as OpenAI Realtime sends one
// (conversation.item.input_audio_transcription.completed).
func inputTranscriptionChunk(transcript string, turnNumber int) providers.StreamChunk {
	return providers.StreamChunk{Metadata: map[string]interface{}{
		keyType:                  "input_transcription",
		"transcription":          transcript,
		providers.InputItemIDKey: mockInputID(turnNumber),
		"content_index":          0,
		"transcription_final":    true,
	}}
}

// mockInputID is the session's ID for the caller's input on turnNumber.
func mockInputID(turnNumber int) string {
	return fmt.Sprintf("mock_input_%d", turnNumber)
}

// sessionRecord is what a session was asked to forget and the notes it was
// given. Its own lock: the pipeline forgets a turn from the goroutine that
// reads the responses, while the session can hold its main lock blocked on
// sending them.
type sessionRecord struct {
	mu     sync.Mutex
	forgot []string
	notes  []string
}

// ForgetTurn implements providers.TurnForgetter: it records inputID, the ID
// the session put on that input's transcription under
// providers.InputItemIDKey.
func (m *MockStreamSession) ForgetTurn(_ context.Context, inputID string) error {
	m.record.mu.Lock()
	defer m.record.mu.Unlock()
	m.record.forgot = append(m.record.forgot, inputID)
	return nil
}

// ForgottenTurns returns the input IDs ForgetTurn was given, in order.
func (m *MockStreamSession) ForgottenTurns() []string {
	m.record.mu.Lock()
	defer m.record.mu.Unlock()
	return append([]string(nil), m.record.forgot...)
}

// AddContextNote implements providers.ContextNoter: it records the note,
// which asks for no reply.
func (m *MockStreamSession) AddContextNote(_ context.Context, text string) error {
	m.record.mu.Lock()
	defer m.record.mu.Unlock()
	m.record.notes = append(m.record.notes, text)
	return nil
}

// ContextNotes returns the notes AddContextNote was given, in order.
func (m *MockStreamSession) ContextNotes() []string {
	m.record.mu.Lock()
	defer m.record.mu.Unlock()
	return append([]string(nil), m.record.notes...)
}

var (
	_ providers.TurnForgetter = (*MockStreamSession)(nil)
	_ providers.ContextNoter  = (*MockStreamSession)(nil)
)
