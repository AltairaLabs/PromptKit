package mock

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests directly exercise emitAutoResponse's failure-mode state machine
// (interruption, provider-closure with/without a final response) and the audio
// framing math. That logic previously lived in a file exempted from the
// coverage gate by its _interactive.go suffix, yet it drives every duplex
// FAILURE-MODE scenario (barge-in, mid-call drop) downstream. A bug here would
// let those resilience tests pass vacuously, so it earns its own direct tests.

// TestEmitAutoResponse_InterruptOnTurn pins the interruption sequence a mock
// session emits when configured to interrupt on a given turn. It mimics
// Gemini detecting user speech mid-response: a partial content chunk, then an
// Interrupted flag, then an empty turnComplete (FinishReason set, no content),
// in exactly that order.
func TestEmitAutoResponse_InterruptOnTurn(t *testing.T) {
	const responseText = "Hello world response" // len 20 -> half is 10 chars
	session := NewMockStreamSession().
		WithAutoRespond(responseText).
		WithInterruptOnTurn(1)

	require.NoError(t, session.SendText(context.Background(), "hi"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	require.Len(t, chunks, 3, "interruption must emit partial + interrupted + empty-complete")

	// 1. Partial content (first half of the response text).
	assert.Equal(t, "Hello worl", chunks[0].Content)
	assert.Equal(t, "Hello worl", chunks[0].Delta)
	assert.False(t, chunks[0].Interrupted)
	assert.Nil(t, chunks[0].FinishReason)

	// 2. Interrupted flag — mimics Gemini serverContent.interrupted.
	assert.True(t, chunks[1].Interrupted)
	assert.Empty(t, chunks[1].Content)
	assert.Nil(t, chunks[1].FinishReason)

	// 3. Empty turnComplete — FinishReason set, no content.
	require.NotNil(t, chunks[2].FinishReason)
	assert.Equal(t, "complete", *chunks[2].FinishReason)
	assert.Empty(t, chunks[2].Content)
	assert.False(t, chunks[2].Interrupted)

	assert.True(t, session.interrupted, "session must record the interruption")
}

// TestEmitAutoResponse_InterruptOnLaterTurn verifies the interruption only
// fires on the configured turn; earlier turns emit a normal response.
func TestEmitAutoResponse_InterruptOnLaterTurn(t *testing.T) {
	session := NewMockStreamSession().
		WithAutoRespond("normal then interrupt").
		WithInterruptOnTurn(2)

	// Turn 1: normal response with a "stop" finish.
	require.NoError(t, session.SendText(context.Background(), "turn 1"))
	turn1 := drainResponses(t, session, 50*time.Millisecond)
	require.Len(t, turn1, 1)
	require.NotNil(t, turn1[0].FinishReason)
	assert.Equal(t, "stop", *turn1[0].FinishReason)
	assert.False(t, turn1[0].Interrupted)
	assert.False(t, session.interrupted)

	// Turn 2: interruption sequence.
	require.NoError(t, session.SendText(context.Background(), "turn 2"))
	turn2 := drainResponses(t, session, 50*time.Millisecond)
	require.Len(t, turn2, 3)
	assert.True(t, turn2[1].Interrupted)
	require.NotNil(t, turn2[2].FinishReason)
	assert.Equal(t, "complete", *turn2[2].FinishReason)
	assert.True(t, session.interrupted)
}

// TestEmitAutoResponse_InterruptEmptyResponseText guards the slice arithmetic
// on an empty responseText (partialText = text[:0]); it must not panic.
func TestEmitAutoResponse_InterruptEmptyResponseText(t *testing.T) {
	session := NewMockStreamSession().
		WithAutoRespond("").
		WithInterruptOnTurn(1)

	require.NoError(t, session.SendText(context.Background(), "hi"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	require.Len(t, chunks, 3)
	assert.Empty(t, chunks[0].Content)
	assert.True(t, chunks[1].Interrupted)
	require.NotNil(t, chunks[2].FinishReason)
}

// TestEmitAutoResponse_CloseAfterTurns pins the "provider drops the connection
// after N successful turns" simulation: N turns each emit a normal response,
// and the response/done channels close after the Nth.
func TestEmitAutoResponse_CloseAfterTurns(t *testing.T) {
	session := NewMockStreamSession().
		WithAutoRespond("bye soon").
		WithCloseAfterTurns(2)

	// Turn 1: normal response, session stays open.
	require.NoError(t, session.SendText(context.Background(), "turn 1"))
	turn1 := drainResponses(t, session, 50*time.Millisecond)
	require.Len(t, turn1, 1)
	require.NotNil(t, turn1[0].FinishReason)
	assert.Equal(t, "stop", *turn1[0].FinishReason)
	assertSessionOpen(t, session)

	// Turn 2: normal response, then the session closes.
	require.NoError(t, session.SendText(context.Background(), "turn 2"))
	turn2 := drainResponses(t, session, 50*time.Millisecond)
	require.Len(t, turn2, 1, "final turn still emits its response before closing")
	require.NotNil(t, turn2[0].FinishReason)
	assert.Equal(t, "stop", *turn2[0].FinishReason)

	assertSessionClosed(t, session)
}

// TestEmitAutoResponse_CloseNoResponseImmediate covers closeNoResponse firing
// on the very first turn: the session closes WITHOUT emitting any chunk,
// mimicking Gemini closing the socket before it ever answers.
func TestEmitAutoResponse_CloseNoResponseImmediate(t *testing.T) {
	session := NewMockStreamSession().
		WithAutoRespond("never sent").
		WithCloseAfterTurns(1, true)

	require.NoError(t, session.SendText(context.Background(), "go"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	assert.Empty(t, chunks, "closeNoResponse must emit no chunks before closing")
	assertSessionClosed(t, session)
	assert.Equal(t, 1, session.responseCount, "the dropped turn still counts")
}

// TestEmitAutoResponse_CloseNoResponseAfterTurn covers closeNoResponse with
// N>1: turn 1 answers normally, turn N is dropped and the session closes
// without a final chunk (mimics Gemini closing after an interrupted
// turnComplete).
func TestEmitAutoResponse_CloseNoResponseAfterTurn(t *testing.T) {
	session := NewMockStreamSession().
		WithAutoRespond("first only").
		WithCloseAfterTurns(2, true)

	// Turn 1: normal response.
	require.NoError(t, session.SendText(context.Background(), "turn 1"))
	turn1 := drainResponses(t, session, 50*time.Millisecond)
	require.Len(t, turn1, 1)
	require.NotNil(t, turn1[0].FinishReason)
	assertSessionOpen(t, session)

	// Turn 2: dropped, session closes with no final chunk.
	require.NoError(t, session.SendText(context.Background(), "turn 2"))
	turn2 := drainResponses(t, session, 50*time.Millisecond)
	assert.Empty(t, turn2, "turn 2 is dropped; no chunk before closure")
	assertSessionClosed(t, session)
}

// TestIsVideoOrImage covers the mime-prefix predicate that gates PCM16
// alignment enforcement in SendChunk.
func TestIsVideoOrImage(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]string
		want bool
	}{
		{"nil meta", nil, false},
		{"empty meta", map[string]string{}, false},
		{"no mime key", map[string]string{"other": "x"}, false},
		{"audio mime", map[string]string{"mime_type": "audio/pcm"}, false},
		{"video mime", map[string]string{"mime_type": "video/h264"}, true},
		{"image mime", map[string]string{"mime_type": "image/png"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isVideoOrImage(tc.meta))
		})
	}
}

// TestConvertMockToolCalls covers the YAML-ToolCall -> runtime-MessageToolCall
// mapping, including the empty short-circuit and the marshal-failure branch
// (a channel value cannot be JSON-marshaled, so the entry is dropped).
func TestConvertMockToolCalls(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		assert.Nil(t, convertMockToolCalls(nil))
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		assert.Nil(t, convertMockToolCalls([]ToolCall{}))
	})

	t.Run("maps name, id and args", func(t *testing.T) {
		out := convertMockToolCalls([]ToolCall{
			{Name: "get_weather", Arguments: map[string]interface{}{"loc": "NYC"}},
		})
		require.Len(t, out, 1)
		assert.Equal(t, "call_0_get_weather", out[0].ID)
		assert.Equal(t, "get_weather", out[0].Name)

		var args map[string]string
		require.NoError(t, json.Unmarshal(out[0].Args, &args))
		assert.Equal(t, "NYC", args["loc"])
	})

	t.Run("drops entries whose args cannot marshal", func(t *testing.T) {
		out := convertMockToolCalls([]ToolCall{
			{Name: "bad", Arguments: map[string]interface{}{"ch": make(chan int)}},
		})
		assert.Empty(t, out, "un-marshalable args must be dropped, not panic")
	})
}

// TestEmitAudioChunks_NilOrEmpty covers the early return for a nil fixture and
// an empty byte slice — neither should emit anything.
func TestEmitAudioChunks_NilOrEmpty(t *testing.T) {
	session := NewMockStreamSession()
	session.emitAudioChunks(nil)
	session.emitAudioChunks(&mockAudioFixture{Bytes: nil})

	select {
	case c := <-session.Response():
		t.Fatalf("expected no chunks, got %+v", c)
	default:
	}
}

// TestEmitAudioChunks_LowSampleRateFallback drives the framing math at a
// sample rate too low for the primary calculation (40Hz: 40*20/1000 == 0).
// Both the fallback divisor (40/50 == 0) and the bytesPerChunk<=0 guard fire,
// so the whole fixture is emitted as a single chunk.
func TestEmitAudioChunks_LowSampleRateFallback(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "low.pcm")
	raw := make([]byte, 8) // 8 arbitrary PCM bytes
	require.NoError(t, os.WriteFile(fixturePath, raw, 0o600))

	repo := newScenarioRepo()
	repo.set("low", 1, &Turn{
		Type:            turnTypeText,
		Content:         "x",
		AudioFile:       "low.pcm",
		AudioSampleRate: 40, // below fallbackFramesPerSecond (50)
	})

	session := NewMockStreamSession().
		WithAutoRespond("fallback").
		WithRepository(repo, dir).
		WithScenarioID("low")

	require.NoError(t, session.SendText(context.Background(), "go"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	var totalBytes int
	mediaCount := 0
	for i := range chunks {
		if chunks[i].MediaData != nil {
			mediaCount++
			totalBytes += len(chunks[i].MediaData.Data)
		}
	}
	require.Equal(t, 1, mediaCount, "low-rate fixture must emit as a single whole-buffer chunk")
	assert.Equal(t, len(raw), totalBytes)
}

// assertSessionOpen fails if the session's done channel is already closed.
func assertSessionOpen(t *testing.T, session *MockStreamSession) {
	t.Helper()
	select {
	case <-session.Done():
		t.Fatal("session should still be open")
	default:
	}
}

// assertSessionClosed fails if the session's done channel has not been closed.
func assertSessionClosed(t *testing.T, session *MockStreamSession) {
	t.Helper()
	select {
	case <-session.Done():
	case <-time.After(50 * time.Millisecond):
		t.Fatal("session should be closed")
	}
}
