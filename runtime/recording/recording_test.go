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

func TestLoad_EventStoreFormat(t *testing.T) {
	// Create a test file in EventStore JSONL format
	tmpDir := t.TempDir()
	sessionID := "test-session-evstore"
	path := filepath.Join(tmpDir, sessionID+".jsonl")

	// Write events in FileEventStore format
	baseTime := time.Now()
	eventLines := []string{
		`{"seq":1,"event":{"type":"audio.input","timestamp":"` + baseTime.Format(time.RFC3339Nano) + `","session_id":"` + sessionID + `","conversation_id":"conv-1","data_type":"*events.AudioInputData","data":{"actor":"user","chunk_index":0,"payload":{"inline_data":"AQID","mime_type":"audio/pcm","size":3},"metadata":{"sample_rate":16000,"channels":1,"encoding":"pcm_linear16","duration_ms":100},"is_final":false}}}`,
		`{"seq":2,"event":{"type":"audio.output","timestamp":"` + baseTime.Add(100*time.Millisecond).Format(time.RFC3339Nano) + `","session_id":"` + sessionID + `","conversation_id":"conv-1","data_type":"*events.AudioOutputData","data":{"chunk_index":0,"payload":{"inline_data":"BAUF","mime_type":"audio/pcm","size":3},"metadata":{"sample_rate":24000,"channels":1,"encoding":"pcm_linear16","duration_ms":50},"generated_from":"model"}}}`,
	}

	content := ""
	for _, line := range eventLines {
		content += line + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	// Load the recording
	rec, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, rec)

	// Verify metadata was synthesized
	assert.Equal(t, sessionID, rec.Metadata.SessionID)
	assert.Equal(t, "conv-1", rec.Metadata.ConversationID)
	assert.Equal(t, recordingVersion, rec.Metadata.Version)
	assert.Equal(t, 2, rec.Metadata.EventCount)

	// Verify events were loaded
	assert.Len(t, rec.Events, 2)
	assert.Equal(t, events.EventAudioInput, rec.Events[0].Type)
	assert.Equal(t, events.EventAudioOutput, rec.Events[1].Type)
	assert.Equal(t, "*events.AudioInputData", rec.Events[0].DataType)
	assert.Equal(t, "*events.AudioOutputData", rec.Events[1].DataType)

	// Verify offsets were calculated
	assert.Equal(t, time.Duration(0), rec.Events[0].Offset)
	assert.True(t, rec.Events[1].Offset > 0)
}

func TestLoad_EventStoreFormat_ToTypedEvents(t *testing.T) {
	// Create a test file in EventStore JSONL format with audio data
	tmpDir := t.TempDir()
	sessionID := "test-typed-events"
	path := filepath.Join(tmpDir, sessionID+".jsonl")

	baseTime := time.Now()
	eventLines := []string{
		`{"seq":1,"event":{"type":"audio.input","timestamp":"` + baseTime.Format(time.RFC3339Nano) + `","session_id":"` + sessionID + `","data_type":"*events.AudioInputData","data":{"actor":"user","chunk_index":0,"payload":{"inline_data":"AQIDBA==","mime_type":"audio/pcm","size":4},"metadata":{"sample_rate":16000,"channels":1}}}}`,
	}

	content := ""
	for _, line := range eventLines {
		content += line + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	// Load and convert to typed events
	rec, err := Load(path)
	require.NoError(t, err)

	typedEvents, err := rec.ToTypedEvents()
	require.NoError(t, err)
	require.Len(t, typedEvents, 1)

	// Verify the data was properly deserialized
	audioData, ok := typedEvents[0].Data.(*events.AudioInputData)
	require.True(t, ok, "expected *events.AudioInputData, got %T", typedEvents[0].Data)
	assert.Equal(t, "user", audioData.Actor)
	assert.Equal(t, 0, audioData.ChunkIndex)
	assert.Equal(t, int64(4), audioData.Payload.Size)
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

func TestDeserializeEventData(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		data     string
		check    func(t *testing.T, result events.EventData, err error)
	}{
		{
			name:     "AudioInputData with pointer prefix",
			dataType: "*events.AudioInputData",
			data:     `{"actor":"user","chunk_index":1}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				data, ok := result.(*events.AudioInputData)
				require.True(t, ok)
				assert.Equal(t, "user", data.Actor)
				assert.Equal(t, 1, data.ChunkIndex)
			},
		},
		{
			name:     "AudioInputData without pointer prefix",
			dataType: "events.AudioInputData",
			data:     `{"actor":"assistant","chunk_index":2}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				data, ok := result.(*events.AudioInputData)
				require.True(t, ok)
				assert.Equal(t, "assistant", data.Actor)
			},
		},
		{
			name:     "StageStartedData",
			dataType: "*events.StageStartedData",
			data:     `{"Name":"test-stage","StageType":"provider"}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				data, ok := result.(*events.StageStartedData)
				require.True(t, ok)
				assert.Equal(t, "test-stage", data.Name)
				assert.Equal(t, "provider", data.StageType)
			},
		},
		{
			name:     "MiddlewareStartedData",
			dataType: "*events.MiddlewareStartedData",
			data:     `{"Name":"logger","Index":0}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				data, ok := result.(*events.MiddlewareStartedData)
				require.True(t, ok)
				assert.Equal(t, "logger", data.Name)
			},
		},
		{
			name:     "ValidationStartedData",
			dataType: "*events.ValidationStartedData",
			data:     `{"ValidatorName":"schema-validator"}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				data, ok := result.(*events.ValidationStartedData)
				require.True(t, ok)
				assert.Equal(t, "schema-validator", data.ValidatorName)
			},
		},
		{
			name:     "ContextBuiltData",
			dataType: "*events.ContextBuiltData",
			data:     `{"TokenCount":1000}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				data, ok := result.(*events.ContextBuiltData)
				require.True(t, ok)
				assert.Equal(t, 1000, data.TokenCount)
			},
		},
		{
			name:     "unknown type returns nil without error",
			dataType: "*events.UnknownType",
			data:     `{"foo":"bar"}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.NoError(t, err)
				assert.Nil(t, result)
			},
		},
		{
			name:     "invalid JSON returns error",
			dataType: "*events.AudioInputData",
			data:     `{invalid json}`,
			check: func(t *testing.T, result events.EventData, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unmarshal")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := deserializeEventData(tt.dataType, json.RawMessage(tt.data))
			tt.check(t, result, err)
		})
	}
}

func TestToTypedEvents_UnknownDataType(t *testing.T) {
	rec := &SessionRecording{
		Metadata: Metadata{Version: "1.0"},
		Events: []RecordedEvent{
			{
				Type:      events.EventMessageCreated,
				Timestamp: time.Now(),
				SessionID: "test-session",
				DataType:  "*events.UnknownEventType",
				Data:      json.RawMessage(`{"unknown":"data"}`),
			},
		},
	}

	typedEvents, err := rec.ToTypedEvents()
	require.NoError(t, err)
	require.Len(t, typedEvents, 1)
	// Unknown types should result in nil Data
	assert.Nil(t, typedEvents[0].Data)
}

func TestLoadJSONLines_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(""), 0600))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty recording file")
}

func TestLoadSessionRecordingFormat_MissingMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-metadata.jsonl")
	// Create file with only events, no metadata line
	content := `{"type":"event","event":{"seq":1,"type":"message.created","timestamp":"2024-01-01T00:00:00Z","session_id":"test"}}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing metadata")
}

func TestLoadEventStoreFormat_EmptyEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-events.jsonl")
	// Create file with only empty lines
	content := "\n\n\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	_, err := Load(path)
	require.Error(t, err)
}

func TestEndToEnd_RecordToMediaTimeline(t *testing.T) {
	// End-to-end test: FileEventStore -> Load -> ToMediaTimeline -> ExportAudioToWAV
	tmpDir := t.TempDir()
	sessionID := "e2e-audio-session"

	// Step 1: Create a FileEventStore and write audio events
	store, err := events.NewFileEventStore(tmpDir)
	require.NoError(t, err)

	baseTime := time.Now()

	// Generate some test PCM audio data (16-bit signed, mono, 16kHz)
	// This creates a simple sine wave pattern for testing
	inputAudio := make([]byte, 3200)  // 100ms of audio at 16kHz, 16-bit mono
	outputAudio := make([]byte, 4800) // 100ms of audio at 24kHz, 16-bit mono
	for i := range inputAudio {
		inputAudio[i] = byte(i % 256)
	}
	for i := range outputAudio {
		outputAudio[i] = byte((i * 2) % 256)
	}

	// Emit audio input events (user speaking)
	inputEvent := &events.Event{
		Type:           events.EventAudioInput,
		Timestamp:      baseTime,
		SessionID:      sessionID,
		ConversationID: "conv-e2e",
		Data: &events.AudioInputData{
			Actor:      "user",
			ChunkIndex: 0,
			Payload: events.BinaryPayload{
				InlineData: inputAudio,
				MIMEType:   "audio/pcm",
				Size:       int64(len(inputAudio)),
			},
			Metadata: events.AudioMetadata{
				SampleRate: 16000,
				Channels:   1,
				Encoding:   "pcm_linear16",
				DurationMs: 100,
			},
			IsFinal: false,
		},
	}
	require.NoError(t, store.Append(context.Background(), inputEvent))

	// Emit audio output events (assistant speaking)
	outputEvent := &events.Event{
		Type:           events.EventAudioOutput,
		Timestamp:      baseTime.Add(200 * time.Millisecond),
		SessionID:      sessionID,
		ConversationID: "conv-e2e",
		Data: &events.AudioOutputData{
			ChunkIndex: 0,
			Payload: events.BinaryPayload{
				InlineData: outputAudio,
				MIMEType:   "audio/pcm",
				Size:       int64(len(outputAudio)),
			},
			Metadata: events.AudioMetadata{
				SampleRate: 24000,
				Channels:   1,
				Encoding:   "pcm_linear16",
				DurationMs: 100,
			},
			GeneratedFrom: "model",
		},
	}
	require.NoError(t, store.Append(context.Background(), outputEvent))
	require.NoError(t, store.Sync())
	store.Close()

	// Step 2: Load the recording from the JSONL file
	recPath := filepath.Join(tmpDir, sessionID+".jsonl")
	rec, err := Load(recPath)
	require.NoError(t, err)
	require.NotNil(t, rec)

	// Verify the recording was loaded correctly
	assert.Equal(t, sessionID, rec.Metadata.SessionID)
	assert.Equal(t, 2, rec.Metadata.EventCount)
	assert.Len(t, rec.Events, 2)

	// Step 3: Convert to MediaTimeline
	timeline, err := rec.ToMediaTimeline(nil) // no blob store needed for inline data
	require.NoError(t, err)
	require.NotNil(t, timeline)

	// Check that we have audio input and output tracks
	inputTrack := timeline.GetTrack(events.TrackAudioInput)
	outputTrack := timeline.GetTrack(events.TrackAudioOutput)
	require.NotNil(t, inputTrack, "expected audio input track")
	require.NotNil(t, outputTrack, "expected audio output track")

	// Verify track contents
	assert.Len(t, inputTrack.Segments, 1)
	assert.Len(t, outputTrack.Segments, 1)

	// Step 4: Export audio to WAV
	inputWavPath := filepath.Join(tmpDir, "input.wav")
	outputWavPath := filepath.Join(tmpDir, "output.wav")

	err = timeline.ExportAudioToWAV(events.TrackAudioInput, inputWavPath)
	require.NoError(t, err)

	err = timeline.ExportAudioToWAV(events.TrackAudioOutput, outputWavPath)
	require.NoError(t, err)

	// Verify WAV files were created
	inputWavInfo, err := os.Stat(inputWavPath)
	require.NoError(t, err)
	assert.True(t, inputWavInfo.Size() > 44, "WAV file should have header + data")

	outputWavInfo, err := os.Stat(outputWavPath)
	require.NoError(t, err)
	assert.True(t, outputWavInfo.Size() > 44, "WAV file should have header + data")

	// Verify WAV file header
	wavData, err := os.ReadFile(inputWavPath)
	require.NoError(t, err)
	assert.Equal(t, "RIFF", string(wavData[0:4]))
	assert.Equal(t, "WAVE", string(wavData[8:12]))
	assert.Equal(t, "fmt ", string(wavData[12:16]))
}
