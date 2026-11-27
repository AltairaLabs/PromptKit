package mock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime provider transcribes the caller's speech and sends the
// transcript on the response stream, where the duplex pipeline runs input
// guardrails on it. The mock sends the transcript a mock response file
// scripts for a spoken turn (input_transcript), ahead of the turn's reply,
// in the shape OpenAI Realtime sends a completed input transcription.

const inputTranscriptConfig = `scenarios:
  guarded:
    turns:
      1:
        content: "Sure, sending it now."
        input_transcript: "a wire transfer please"
      2:
        content: "It is noon."
        input_transcript: "what time is it"
      3: "no transcript scripted"
`

// inputTranscriptSession is a session of a streaming provider reading
// inputTranscriptConfig from a mock response file.
func inputTranscriptSession(t *testing.T) *MockStreamSession {
	t.Helper()
	repo, err := NewFileMockRepository(writeMockConfig(t, inputTranscriptConfig))
	require.NoError(t, err)
	p := NewStreamingProvider("mock", "m", false).WithAutoRespond("fallback").
		WithMockResponses(repo, "guarded", repo.BaseDir())
	sess, err := p.CreateStreamSession(context.Background(), &providers.StreamingInputConfig{})
	require.NoError(t, err)
	return sess.(*MockStreamSession)
}

// speak sends one spoken caller turn: audio, then the end of input.
func speak(t *testing.T, s *MockStreamSession) {
	t.Helper()
	require.NoError(t, s.SendChunk(context.Background(), &types.MediaChunk{Data: make([]byte, 320)}))
	s.EndInput()
}

// turnChunks reads the chunks of one turn, up to and including the one with
// its finish reason.
func turnChunks(t *testing.T, s *MockStreamSession) []providers.StreamChunk {
	t.Helper()
	var out []providers.StreamChunk
	for {
		select {
		case c := <-s.Response():
			out = append(out, c)
			if c.FinishReason != nil {
				return out
			}
		case <-time.After(time.Second):
			require.FailNow(t, "the turn never finished", "got %v", out)
		}
	}
}

// inputTranscription is the chunk the mock sends for the caller's input id.
func inputTranscription(text, id string) map[string]interface{} {
	return map[string]interface{}{
		"type":                   "input_transcription",
		"transcription":          text,
		providers.InputItemIDKey: id,
		"content_index":          0,
		"transcription_final":    true,
	}
}

// Each spoken turn's scripted transcript comes ahead of its reply, as a
// final input transcription naming that turn's input.
func TestMockStreamSession_SpokenTurnSendsItsScriptedTranscriptBeforeTheReply(t *testing.T) {
	s := inputTranscriptSession(t)

	speak(t, s)
	first := turnChunks(t, s)
	require.Len(t, first, 2)
	assert.Equal(t, inputTranscription("a wire transfer please", "mock_input_1"), first[0].Metadata)
	assert.Empty(t, first[0].Content, "the transcript is not reply text")
	assert.Equal(t, "Sure, sending it now.", first[1].Content)

	speak(t, s)
	second := turnChunks(t, s)
	require.Len(t, second, 2)
	assert.Equal(t, inputTranscription("what time is it", "mock_input_2"), second[0].Metadata)
	assert.Equal(t, "It is noon.", second[1].Content)
}

// A turn that scripts no transcript gets none, and a typed turn is not
// transcribed: a realtime provider transcribes only audio.
func TestMockStreamSession_OnlyAScriptedSpokenTurnIsTranscribed(t *testing.T) {
	s := inputTranscriptSession(t)

	speak(t, s)
	require.Len(t, turnChunks(t, s), 2, "the spoken turn is transcribed")

	require.NoError(t, s.SendText(context.Background(), "typed"))
	typed := turnChunks(t, s)
	require.Len(t, typed, 1, "a typed turn after a spoken one gets its reply only")
	assert.Equal(t, "It is noon.", typed[0].Content)

	speak(t, s)
	unscripted := turnChunks(t, s)
	require.Len(t, unscripted, 1, "a turn without input_transcript gets its reply only")
	assert.Equal(t, "no transcript scripted", unscripted[0].Content)
}

// A session given a response file but no scenario reads nothing from the
// file: it answers with its own text and sends no transcript.
func TestMockStreamSession_NoScenarioReadsNothingFromTheFile(t *testing.T) {
	repo, err := NewFileMockRepository(writeMockConfig(t, "defaultResponse: from the file\n"+inputTranscriptConfig))
	require.NoError(t, err)
	p := NewStreamingProvider("mock", "m", false).WithAutoRespond("its own text").
		WithMockResponses(repo, "", repo.BaseDir())
	sess, err := p.CreateStreamSession(context.Background(), &providers.StreamingInputConfig{})
	require.NoError(t, err)
	s := sess.(*MockStreamSession)

	speak(t, s)
	chunks := turnChunks(t, s)
	require.Len(t, chunks, 1)
	assert.Equal(t, "its own text", chunks[0].Content)
}

// The session records the turns it is asked to forget and the notes it is
// given, in order, and sends nothing for either: neither asks for a reply.
func TestMockStreamSession_RecordsForgottenTurnsAndContextNotes(t *testing.T) {
	s := NewMockStreamSession()
	var f providers.TurnForgetter = s
	var n providers.ContextNoter = s

	require.NoError(t, f.ForgetTurn(context.Background(), "mock_input_2"))
	require.NoError(t, f.ForgetTurn(context.Background(), "mock_input_1"))
	require.NoError(t, n.AddContextNote(context.Background(), "a note"))

	assert.Equal(t, []string{"mock_input_2", "mock_input_1"}, s.ForgottenTurns())
	assert.Equal(t, []string{"a note"}, s.ContextNotes())
	assert.Empty(t, s.GetTexts(), "a note is not caller input")
	select {
	case c := <-s.Response():
		assert.Fail(t, "a forget or a note sent a chunk", "%+v", c)
	default:
	}
}

// A misspelled transcript key is rejected and the error names the real one.
func TestNewFileMockRepository_NamesInputTranscriptAmongTheTurnKeys(t *testing.T) {
	_, err := NewFileMockRepository(writeMockConfig(t,
		"scenarios:\n  s1:\n    turns:\n      1:\n        input_transcription: hi\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown key "input_transcription"`)
	assert.Contains(t, err.Error(), "input_transcript,")
}
