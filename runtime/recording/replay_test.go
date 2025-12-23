package recording

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestRecordingWithMessages(t *testing.T) *SessionRecording {
	t.Helper()

	tmpDir := t.TempDir()
	store, err := events.NewFileEventStore(tmpDir)
	require.NoError(t, err)

	sessionID := "replay-test-session"
	baseTime := time.Now()

	// Create a sequence of events
	testEvents := []*events.Event{
		{
			Type:           events.EventConversationStarted,
			Timestamp:      baseTime,
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.ConversationStartedData{
				SystemPrompt: "You are a helpful assistant.",
			},
		},
		{
			Type:           events.EventMessageCreated,
			Timestamp:      baseTime.Add(100 * time.Millisecond),
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.MessageCreatedData{
				Role:    "user",
				Content: "Hello!",
				Index:   0,
			},
		},
		{
			Type:           events.EventAudioInput,
			Timestamp:      baseTime.Add(200 * time.Millisecond),
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.AudioInputData{
				Actor:      "user",
				ChunkIndex: 0,
				Payload: events.BinaryPayload{
					InlineData: []byte{0x01, 0x02, 0x03, 0x04},
					MIMEType:   "audio/pcm",
					Size:       4,
				},
				Metadata: events.AudioMetadata{
					SampleRate: 16000,
					Channels:   1,
					DurationMs: 100,
				},
			},
		},
		{
			Type:           events.EventMessageCreated,
			Timestamp:      baseTime.Add(500 * time.Millisecond),
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.MessageCreatedData{
				Role:    "assistant",
				Content: "Hi there! How can I help you?",
				Index:   1,
			},
		},
		{
			Type:           events.EventAudioOutput,
			Timestamp:      baseTime.Add(600 * time.Millisecond),
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.AudioOutputData{
				ChunkIndex: 0,
				Payload: events.BinaryPayload{
					InlineData: []byte{0xAA, 0xBB, 0xCC, 0xDD},
					MIMEType:   "audio/pcm",
					Size:       4,
				},
				Metadata: events.AudioMetadata{
					SampleRate: 24000,
					Channels:   1,
					DurationMs: 100,
				},
				GeneratedFrom: "model",
			},
		},
		{
			Type:           events.EventMessageCreated,
			Timestamp:      baseTime.Add(1 * time.Second),
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.MessageCreatedData{
				Role:    "user",
				Content: "Tell me a joke.",
				Index:   2,
			},
		},
	}

	for _, event := range testEvents {
		require.NoError(t, store.Append(context.Background(), event))
	}
	require.NoError(t, store.Sync())
	store.Close()

	// Load the recording
	rec, err := Load(tmpDir + "/" + sessionID + ".jsonl")
	require.NoError(t, err)

	return rec
}

func TestReplayPlayer_Basic(t *testing.T) {
	rec := createTestRecordingWithMessages(t)

	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Initial position should be 0
	assert.Equal(t, time.Duration(0), player.Position())

	// Duration should match recording
	assert.Equal(t, rec.Metadata.Duration, player.Duration())

	// Should have timeline
	assert.NotNil(t, player.Timeline())
	assert.NotNil(t, player.Recording())
}

func TestReplayPlayer_Seek(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Seek to middle
	player.Seek(500 * time.Millisecond)
	assert.Equal(t, 500*time.Millisecond, player.Position())

	// Seek past end should clamp
	player.Seek(10 * time.Second)
	assert.Equal(t, rec.Metadata.Duration, player.Position())

	// Seek to negative should clamp to 0
	player.Seek(-1 * time.Second)
	assert.Equal(t, time.Duration(0), player.Position())
}

func TestReplayPlayer_GetState(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Get state at start
	state := player.GetStateAt(0)
	assert.Equal(t, time.Duration(0), state.Position)
	assert.NotZero(t, state.Timestamp)

	// Get state at 500ms - should have messages up to that point
	state = player.GetStateAt(500 * time.Millisecond)
	assert.Len(t, state.Messages, 2) // User "Hello!" and assistant response

	// Get state at 1s - should have all 3 messages
	state = player.GetStateAt(1 * time.Second)
	assert.Len(t, state.Messages, 3)
}

func TestReplayPlayer_GetEventsInRange(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Get all events
	allEvents := player.GetEventsInRange(0, 2*time.Second)
	assert.Len(t, allEvents, 6) // All events

	// Get events in first 300ms
	earlyEvents := player.GetEventsInRange(0, 300*time.Millisecond)
	assert.True(t, len(earlyEvents) >= 3) // conversation.started, message.created, audio.input
}

func TestReplayPlayer_GetEventsByType(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Get all message events
	messages := player.GetEventsByType(events.EventMessageCreated)
	assert.Len(t, messages, 3)

	// Get audio input events
	audioIn := player.GetEventsByType(events.EventAudioInput)
	assert.Len(t, audioIn, 1)
}

func TestReplayPlayer_Advance(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Advance 200ms
	events := player.Advance(200 * time.Millisecond)
	assert.True(t, len(events) >= 2) // Should have events in first 200ms
	assert.Equal(t, 200*time.Millisecond, player.Position())

	// Advance another 300ms
	events = player.Advance(300 * time.Millisecond)
	assert.True(t, len(events) >= 1) // More events
	assert.Equal(t, 500*time.Millisecond, player.Position())
}

func TestReplayPlayer_AdvanceTo(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Advance to 500ms
	events := player.AdvanceTo(500 * time.Millisecond)
	assert.True(t, len(events) >= 3)
	assert.Equal(t, 500*time.Millisecond, player.Position())

	// Advance to earlier time should return nil and just move position
	events = player.AdvanceTo(200 * time.Millisecond)
	assert.Nil(t, events)
	assert.Equal(t, 200*time.Millisecond, player.Position())
}

func TestReplayPlayer_WithAnnotations(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Add some annotations
	sessionAnn := &annotations.Annotation{
		ID:        "ann-1",
		Type:      annotations.TypeScore,
		SessionID: rec.Metadata.SessionID,
		Target:    annotations.ForSession(),
		Key:       "quality",
		Value:     annotations.NewScoreValue(0.9),
	}

	rangeAnn := &annotations.Annotation{
		ID:        "ann-2",
		Type:      annotations.TypeComment,
		SessionID: rec.Metadata.SessionID,
		Target: annotations.InTimeRange(
			rec.Metadata.StartTime.Add(200*time.Millisecond),
			rec.Metadata.StartTime.Add(700*time.Millisecond),
		),
		Key:   "note",
		Value: annotations.NewCommentValue("Important section"),
	}

	player.SetAnnotations([]*annotations.Annotation{sessionAnn, rangeAnn})

	// Session annotation should always be active
	state := player.GetStateAt(0)
	assert.Len(t, state.ActiveAnnotations, 1)

	// At 500ms, both annotations should be active
	state = player.GetStateAt(500 * time.Millisecond)
	assert.Len(t, state.ActiveAnnotations, 2)

	// At 1s, only session annotation
	state = player.GetStateAt(1 * time.Second)
	assert.Len(t, state.ActiveAnnotations, 1)
}

func TestReplayPlayer_FormatPosition(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	player.Seek(1500 * time.Millisecond)
	pos := player.FormatPosition()

	// Should be in MM:SS.mmm format
	assert.Contains(t, pos, ":")
	assert.Contains(t, pos, "/")
}

func TestReplayPlayer_EventIterator(t *testing.T) {
	rec := createTestRecordingWithMessages(t)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	// Iterate over all events
	iter := player.NewEventIterator(0, 2*time.Second)

	count := 0
	for {
		_, ok := iter.Next()
		if !ok {
			break
		}
		count++
	}
	assert.Equal(t, 6, count)

	// Iterate over subset
	iter = player.NewEventIterator(100*time.Millisecond, 600*time.Millisecond)
	count = 0
	for {
		_, ok := iter.Next()
		if !ok {
			break
		}
		count++
	}
	assert.True(t, count >= 3 && count <= 5)
}
