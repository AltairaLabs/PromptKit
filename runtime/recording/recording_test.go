package recording

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestStore(t *testing.T, eventCount int, interval time.Duration) (*events.FileEventStore, string) {
	t.Helper()
	store, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)

	sessionID := "test-session-" + t.Name()
	baseTime := time.Now()

	for i := 0; i < eventCount; i++ {
		event := &events.Event{
			Type:           events.EventMessageCreated,
			Timestamp:      baseTime.Add(time.Duration(i) * interval),
			SessionID:      sessionID,
			ConversationID: "conv-1",
			Data: &events.MessageCreatedData{
				Role:    "user",
				Content: "Message " + string(rune('A'+i)),
			},
		}
		require.NoError(t, store.Append(context.Background(), event))
	}
	require.NoError(t, store.Sync())

	return store, sessionID
}

func TestExport(t *testing.T) {
	store, sessionID := createTestStore(t, 5, 100*time.Millisecond)
	defer store.Close()

	rec, err := Export(context.Background(), store, sessionID)
	require.NoError(t, err)
	require.NotNil(t, rec)

	t.Run("metadata is populated", func(t *testing.T) {
		assert.Equal(t, sessionID, rec.Metadata.SessionID)
		assert.Equal(t, "conv-1", rec.Metadata.ConversationID)
		assert.Equal(t, 5, rec.Metadata.EventCount)
		assert.Equal(t, recordingVersion, rec.Metadata.Version)
		assert.False(t, rec.Metadata.StartTime.IsZero())
		assert.False(t, rec.Metadata.EndTime.IsZero())
		assert.InDelta(t, 400*time.Millisecond, rec.Metadata.Duration, float64(10*time.Millisecond))
	})

	t.Run("events are captured", func(t *testing.T) {
		assert.Len(t, rec.Events, 5)
		for i, e := range rec.Events {
			assert.Equal(t, int64(i+1), e.Sequence)
			assert.Equal(t, events.EventMessageCreated, e.Type)
			assert.Equal(t, sessionID, e.SessionID)
			assert.NotEmpty(t, e.Data)
			assert.Equal(t, "*events.MessageCreatedData", e.DataType)
		}
	})

	t.Run("events have correct offsets", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), rec.Events[0].Offset)
		assert.InDelta(t, 100*time.Millisecond, rec.Events[1].Offset, float64(10*time.Millisecond))
		assert.InDelta(t, 200*time.Millisecond, rec.Events[2].Offset, float64(10*time.Millisecond))
	})
}

func TestExport_EmptySession(t *testing.T) {
	store, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	_, err = Export(context.Background(), store, "nonexistent-session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no events found")
}

func TestExportWithOptions(t *testing.T) {
	store, sessionID := createTestStore(t, 3, 50*time.Millisecond)
	defer store.Close()

	rec, err := ExportWithOptions(context.Background(), store, sessionID, ExportOptions{
		ProviderName: "openai",
		Model:        "gpt-4o",
		Custom: map[string]any{
			"test_run": true,
			"user_id":  "user-123",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "openai", rec.Metadata.ProviderName)
	assert.Equal(t, "gpt-4o", rec.Metadata.Model)
	assert.Equal(t, true, rec.Metadata.Custom["test_run"])
	assert.Equal(t, "user-123", rec.Metadata.Custom["user_id"])
}

func TestSaveAndLoad_JSON(t *testing.T) {
	store, sessionID := createTestStore(t, 5, 100*time.Millisecond)
	defer store.Close()

	rec, err := Export(context.Background(), store, sessionID)
	require.NoError(t, err)

	// Save to file
	path := filepath.Join(t.TempDir(), "test.recording.json")
	require.NoError(t, rec.SaveTo(path, FormatJSON))

	// Verify file exists
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load it back
	loaded, err := Load(path)
	require.NoError(t, err)

	// Verify loaded data matches
	assert.Equal(t, rec.Metadata.SessionID, loaded.Metadata.SessionID)
	assert.Equal(t, rec.Metadata.EventCount, loaded.Metadata.EventCount)
	assert.Equal(t, rec.Metadata.Version, loaded.Metadata.Version)
	assert.Len(t, loaded.Events, 5)

	for i, e := range loaded.Events {
		assert.Equal(t, rec.Events[i].Sequence, e.Sequence)
		assert.Equal(t, rec.Events[i].Type, e.Type)
	}
}

func TestSaveAndLoad_JSONL(t *testing.T) {
	store, sessionID := createTestStore(t, 5, 100*time.Millisecond)
	defer store.Close()

	rec, err := Export(context.Background(), store, sessionID)
	require.NoError(t, err)

	// Save to file
	path := filepath.Join(t.TempDir(), "test.recording.jsonl")
	require.NoError(t, rec.SaveTo(path, FormatJSONLines))

	// Load it back
	loaded, err := Load(path)
	require.NoError(t, err)

	// Verify loaded data matches
	assert.Equal(t, rec.Metadata.SessionID, loaded.Metadata.SessionID)
	assert.Equal(t, rec.Metadata.EventCount, loaded.Metadata.EventCount)
	assert.Len(t, loaded.Events, 5)
}

func TestSaveTo_InvalidFormat(t *testing.T) {
	rec := &SessionRecording{
		Metadata: Metadata{Version: "1.0"},
	}

	err := rec.SaveTo("/tmp/test.txt", Format("invalid"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestLoad_InvalidFile(t *testing.T) {
	// Non-existent file
	_, err := Load("/nonexistent/path/recording.json")
	require.Error(t, err)

	// Invalid JSON
	path := filepath.Join(t.TempDir(), "invalid.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))
	_, err = Load(path)
	require.Error(t, err)
}

func TestToEvents(t *testing.T) {
	store, sessionID := createTestStore(t, 3, 50*time.Millisecond)
	defer store.Close()

	rec, err := Export(context.Background(), store, sessionID)
	require.NoError(t, err)

	events := rec.ToEvents()
	assert.Len(t, events, 3)

	for i, e := range events {
		assert.Equal(t, rec.Events[i].Type, e.Type)
		assert.Equal(t, rec.Events[i].SessionID, e.SessionID)
		assert.Equal(t, rec.Events[i].Timestamp, e.Timestamp)
	}
}

func TestDuration(t *testing.T) {
	rec := &SessionRecording{
		Metadata: Metadata{
			Duration: 5 * time.Minute,
		},
	}
	assert.Equal(t, 5*time.Minute, rec.Duration())
}

func TestString(t *testing.T) {
	rec := &SessionRecording{
		Metadata: Metadata{
			SessionID:  "test-123",
			EventCount: 42,
			Duration:   2 * time.Minute,
		},
	}

	s := rec.String()
	assert.Contains(t, s, "test-123")
	assert.Contains(t, s, "42")
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty", "", nil},
		{"single line no newline", "hello", []string{"hello"}},
		{"single line with newline", "hello\n", []string{"hello"}},
		{"multiple lines", "a\nb\nc", []string{"a", "b", "c"}},
		{"multiple lines with trailing newline", "a\nb\nc\n", []string{"a", "b", "c"}},
		{"empty lines", "a\n\nb\n", []string{"a", "", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines([]byte(tt.input))
			var strResult []string
			for _, b := range result {
				strResult = append(strResult, string(b))
			}
			assert.Equal(t, tt.expected, strResult)
		})
	}
}

func TestRecordedEvent_PreservesRawJSON(t *testing.T) {
	store, sessionID := createTestStore(t, 1, 0)
	defer store.Close()

	rec, err := Export(context.Background(), store, sessionID)
	require.NoError(t, err)
	require.Len(t, rec.Events, 1)

	// The data should be valid JSON that can be unmarshaled
	event := rec.Events[0]
	assert.NotEmpty(t, event.Data)
	assert.NotEmpty(t, event.DataType)

	// Verify it contains the expected content (MessageCreatedData has Role and Content fields)
	var data events.MessageCreatedData
	require.NoError(t, json.Unmarshal(event.Data, &data))
	assert.Equal(t, "user", data.Role)
	assert.Equal(t, "Message A", data.Content)
}
