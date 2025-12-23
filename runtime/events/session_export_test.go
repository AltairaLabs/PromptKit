package events

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultExportConfig(t *testing.T) {
	config := DefaultExportConfig("/tmp/output.mp4")

	assert.Equal(t, ExportFormatMP4, config.Format)
	assert.Equal(t, "/tmp/output.mp4", config.OutputPath)
	assert.True(t, config.IncludeAnnotations)
	assert.False(t, config.IncludeEvents)
	assert.True(t, config.IncludeTranscriptions)
	assert.Equal(t, defaultVideoWidth, config.VideoWidth)
	assert.Equal(t, defaultVideoHeight, config.VideoHeight)
	assert.Equal(t, defaultFontSize, config.FontSize)
	assert.Equal(t, audioMixStereo, config.AudioMix)
	assert.Equal(t, defaultFFmpeg, config.FFmpegPath)
}

func TestNewSessionExporter(t *testing.T) {
	session := createTestAnnotatedSession(t)

	t.Run("with nil config uses defaults", func(t *testing.T) {
		exporter := NewSessionExporter(session, nil)
		require.NotNil(t, exporter)
		assert.Equal(t, defaultVideoWidth, exporter.config.VideoWidth)
		assert.Equal(t, defaultVideoHeight, exporter.config.VideoHeight)
		assert.Equal(t, defaultFontSize, exporter.config.FontSize)
		assert.Equal(t, defaultFFmpeg, exporter.config.FFmpegPath)
		assert.Equal(t, audioMixStereo, exporter.config.AudioMix)
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &ExportConfig{
			Format:      ExportFormatJSON,
			OutputPath:  "/tmp/test.json",
			VideoWidth:  1920,
			VideoHeight: 1080,
			FontSize:    32,
			FFmpegPath:  "/custom/ffmpeg",
			AudioMix:    audioMixMono,
		}
		exporter := NewSessionExporter(session, config)
		require.NotNil(t, exporter)
		assert.Equal(t, 1920, exporter.config.VideoWidth)
		assert.Equal(t, 1080, exporter.config.VideoHeight)
		assert.Equal(t, 32, exporter.config.FontSize)
		assert.Equal(t, "/custom/ffmpeg", exporter.config.FFmpegPath)
		assert.Equal(t, audioMixMono, exporter.config.AudioMix)
	})

	t.Run("fills in zero values with defaults", func(t *testing.T) {
		config := &ExportConfig{
			Format:     ExportFormatJSON,
			OutputPath: "/tmp/test.json",
			// Leave all other fields as zero
		}
		exporter := NewSessionExporter(session, config)
		require.NotNil(t, exporter)
		assert.Equal(t, defaultVideoWidth, exporter.config.VideoWidth)
		assert.Equal(t, defaultVideoHeight, exporter.config.VideoHeight)
		assert.Equal(t, defaultFontSize, exporter.config.FontSize)
		assert.Equal(t, defaultFFmpeg, exporter.config.FFmpegPath)
		assert.Equal(t, audioMixStereo, exporter.config.AudioMix)
	})
}

func TestSessionExporter_Export_JSON(t *testing.T) {
	session := createTestAnnotatedSession(t)
	outputPath := filepath.Join(t.TempDir(), "timeline.json")

	config := &ExportConfig{
		Format:     ExportFormatJSON,
		OutputPath: outputPath,
	}

	exporter := NewSessionExporter(session, config)
	err := exporter.Export(context.Background())
	require.NoError(t, err)

	// Verify file was created
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	// Verify it's valid JSON
	var timeline map[string]interface{}
	err = json.Unmarshal(data, &timeline)
	require.NoError(t, err)

	// Verify structure
	assert.Contains(t, timeline, "session_id")
	assert.Contains(t, timeline, "duration_seconds")
	assert.Contains(t, timeline, "events")
	assert.Contains(t, timeline, "metadata")
	assert.Contains(t, timeline, "tracks")
}

func TestSessionExporter_Export_UnsupportedFormat(t *testing.T) {
	session := createTestAnnotatedSession(t)

	config := &ExportConfig{
		Format:     ExportFormat("unsupported"),
		OutputPath: "/tmp/test.out",
	}

	exporter := NewSessionExporter(session, config)
	err := exporter.Export(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestSessionExporter_Export_ContextCancellation(t *testing.T) {
	session := createTestAnnotatedSession(t)
	outputPath := filepath.Join(t.TempDir(), "timeline.json")

	config := &ExportConfig{
		Format:     ExportFormatJSON,
		OutputPath: outputPath,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	exporter := NewSessionExporter(session, config)
	err := exporter.Export(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWantsInputAudio(t *testing.T) {
	tests := []struct {
		name     string
		mix      string
		expected bool
	}{
		{"stereo wants input", audioMixStereo, true},
		{"mono wants input", audioMixMono, true},
		{"input only wants input", audioMixInput, true},
		{"output only does not want input", audioMixOutput, false},
		{"empty returns false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ExportConfig{AudioMix: tt.mix}
			exporter := &SessionExporter{config: config}
			assert.Equal(t, tt.expected, exporter.wantsInputAudio())
		})
	}
}

func TestWantsOutputAudio(t *testing.T) {
	tests := []struct {
		name     string
		mix      string
		expected bool
	}{
		{"stereo wants output", audioMixStereo, true},
		{"mono wants output", audioMixMono, true},
		{"output only wants output", audioMixOutput, true},
		{"input only does not want output", audioMixInput, false},
		{"empty returns false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ExportConfig{AudioMix: tt.mix}
			exporter := &SessionExporter{config: config}
			assert.Equal(t, tt.expected, exporter.wantsOutputAudio())
		})
	}
}

func TestFormatSRTTime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"zero", 0, "00:00:00,000"},
		{"one second", time.Second, "00:00:01,000"},
		{"one minute", time.Minute, "00:01:00,000"},
		{"one hour", time.Hour, "01:00:00,000"},
		{"complex time", time.Hour + 23*time.Minute + 45*time.Second + 678*time.Millisecond, "01:23:45,678"},
		{"milliseconds only", 500 * time.Millisecond, "00:00:00,500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSRTTime(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSessionExporter_FormatAnnotation(t *testing.T) {
	session := createTestAnnotatedSession(t)
	exporter := NewSessionExporter(session, nil)

	t.Run("score annotation", func(t *testing.T) {
		score := 0.85
		annot := &annotations.Annotation{
			Type:  annotations.TypeScore,
			Value: annotations.AnnotationValue{Score: &score},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "0.85", result)
	})

	t.Run("label annotation", func(t *testing.T) {
		annot := &annotations.Annotation{
			Type:  annotations.TypeLabel,
			Value: annotations.AnnotationValue{Label: "test-label"},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "test-label", result)
	})

	t.Run("comment annotation", func(t *testing.T) {
		annot := &annotations.Annotation{
			Type:  annotations.TypeComment,
			Value: annotations.AnnotationValue{Text: "This is a comment"},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "This is a comment", result)
	})

	t.Run("score annotation with nil value", func(t *testing.T) {
		annot := &annotations.Annotation{
			Type:  annotations.TypeScore,
			Value: annotations.AnnotationValue{Score: nil},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "", result)
	})
}

func TestGetAudioFormat(t *testing.T) {
	t.Run("returns default sample rate and channels", func(t *testing.T) {
		session := createTestAnnotatedSession(t)
		config := &ExportConfig{}
		exporter := &SessionExporter{config: config, session: session}

		sampleRate, channels := exporter.getAudioFormat()

		// Default values when no audio track exists
		assert.Equal(t, 24000, sampleRate)
		assert.Equal(t, 1, channels)
	})
}

func TestGetAnnotationTime(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	session := &AnnotatedSession{
		SessionID: "test-session",
		Metadata: SessionMetadata{
			StartTime: baseTime,
			Duration:  10 * time.Second,
		},
		Events: []*Event{
			{Timestamp: baseTime},
			{Timestamp: baseTime.Add(2 * time.Second)},
			{Timestamp: baseTime.Add(5 * time.Second)},
		},
	}
	exporter := &SessionExporter{session: session}

	t.Run("time range target", func(t *testing.T) {
		annot := &annotations.Annotation{
			Target: annotations.Target{
				Type:      annotations.TargetTimeRange,
				StartTime: baseTime.Add(5 * time.Second),
			},
		}
		result := exporter.getAnnotationTime(annot)
		assert.Equal(t, 5*time.Second, result)
	})

	t.Run("session target returns zero", func(t *testing.T) {
		annot := &annotations.Annotation{
			Target: annotations.Target{
				Type: annotations.TargetSession,
			},
		}
		result := exporter.getAnnotationTime(annot)
		assert.Equal(t, time.Duration(0), result)
	})

	t.Run("event target returns event time", func(t *testing.T) {
		annot := &annotations.Annotation{
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 1,
			},
		}
		result := exporter.getAnnotationTime(annot)
		assert.Equal(t, 2*time.Second, result)
	})

	t.Run("event target with invalid sequence returns zero", func(t *testing.T) {
		annot := &annotations.Annotation{
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 100, // Out of range
			},
		}
		result := exporter.getAnnotationTime(annot)
		assert.Equal(t, time.Duration(0), result)
	})
}

func TestBuildJSONTimeline(t *testing.T) {
	session := createTestAnnotatedSession(t)
	exporter := NewSessionExporter(session, nil)

	timeline := exporter.buildJSONTimeline()

	assert.Equal(t, session.SessionID, timeline.SessionID)
	assert.True(t, timeline.Duration >= 0)
	assert.NotNil(t, timeline.Events)
	// Tracks may be nil if no audio tracks exist
}

func TestBuildJSONTimeline_WithDifferentEventTypes(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-session-events"

	// Create events with different types
	testEvents := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: baseTime,
			SessionID: sessionID,
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventAudioTranscription,
			Timestamp: baseTime.Add(100 * time.Millisecond),
			SessionID: sessionID,
			Data:      &AudioTranscriptionData{Text: "Hello world", Language: "en-US"},
		},
		{
			Type:      EventToolCallStarted,
			Timestamp: baseTime.Add(200 * time.Millisecond),
			SessionID: sessionID,
			Data:      &ToolCallStartedData{ToolName: "calculator"},
		},
		{
			Type:      EventProviderCallCompleted,
			Timestamp: baseTime.Add(300 * time.Millisecond),
			SessionID: sessionID,
			Data:      &ProviderCallCompletedData{Provider: "openai", Model: "gpt-4", Cost: 0.01},
		},
	}

	session := &AnnotatedSession{
		SessionID: sessionID,
		Events:    testEvents,
		Timeline:  NewMediaTimeline(sessionID, testEvents, nil),
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(300 * time.Millisecond),
			Duration:  300 * time.Millisecond,
		},
	}

	exporter := NewSessionExporter(session, nil)
	timeline := exporter.buildJSONTimeline()

	assert.Len(t, timeline.Events, 4)

	// Check MessageCreatedData
	assert.Equal(t, "message.created", timeline.Events[0].Type)
	assert.Equal(t, "user", timeline.Events[0].Data["role"])
	assert.Equal(t, "Hello", timeline.Events[0].Data["content"])

	// Check AudioTranscriptionData
	assert.Equal(t, "audio.transcription", timeline.Events[1].Type)
	assert.Equal(t, "Hello world", timeline.Events[1].Data["text"])
	assert.Equal(t, "en-US", timeline.Events[1].Data["language"])

	// Check ToolCallStartedData
	assert.Equal(t, "tool.call.started", timeline.Events[2].Type)
	assert.Equal(t, "calculator", timeline.Events[2].Data["tool_name"])

	// Check ProviderCallCompletedData
	assert.Equal(t, "provider.call.completed", timeline.Events[3].Type)
	assert.Equal(t, "openai", timeline.Events[3].Data["provider"])
	assert.Equal(t, "gpt-4", timeline.Events[3].Data["model"])
	assert.Equal(t, 0.01, timeline.Events[3].Data["cost"])
}

func TestBuildJSONTimeline_WithTimeFiltering(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-session-filtered"

	testEvents := []*Event{
		{Type: EventMessageCreated, Timestamp: baseTime, SessionID: sessionID, Data: &MessageCreatedData{Role: "user", Content: "1"}},
		{Type: EventMessageCreated, Timestamp: baseTime.Add(time.Second), SessionID: sessionID, Data: &MessageCreatedData{Role: "user", Content: "2"}},
		{Type: EventMessageCreated, Timestamp: baseTime.Add(2 * time.Second), SessionID: sessionID, Data: &MessageCreatedData{Role: "user", Content: "3"}},
		{Type: EventMessageCreated, Timestamp: baseTime.Add(3 * time.Second), SessionID: sessionID, Data: &MessageCreatedData{Role: "user", Content: "4"}},
	}

	session := &AnnotatedSession{
		SessionID: sessionID,
		Events:    testEvents,
		Timeline:  NewMediaTimeline(sessionID, testEvents, nil),
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(3 * time.Second),
			Duration:  3 * time.Second,
		},
	}

	t.Run("filter by start time", func(t *testing.T) {
		config := &ExportConfig{
			StartTime: time.Second + 500*time.Millisecond, // After first two events
		}
		exporter := NewSessionExporter(session, config)
		timeline := exporter.buildJSONTimeline()

		// Should have events 3 and 4 (at 2s and 3s)
		assert.Len(t, timeline.Events, 2)
	})

	t.Run("filter by end time", func(t *testing.T) {
		config := &ExportConfig{
			EndTime: time.Second + 500*time.Millisecond, // Before third event
		}
		exporter := NewSessionExporter(session, config)
		timeline := exporter.buildJSONTimeline()

		// Should have events 1 and 2 (at 0s and 1s)
		assert.Len(t, timeline.Events, 2)
	})

	t.Run("filter by both start and end time", func(t *testing.T) {
		config := &ExportConfig{
			StartTime: 500 * time.Millisecond,
			EndTime:   2*time.Second + 500*time.Millisecond,
		}
		exporter := NewSessionExporter(session, config)
		timeline := exporter.buildJSONTimeline()

		// Should have events 2 and 3 (at 1s and 2s)
		assert.Len(t, timeline.Events, 2)
	})
}

func TestBuildJSONTimeline_WithAnnotations(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-session-annot"

	testEvents := []*Event{
		{Type: EventMessageCreated, Timestamp: baseTime, SessionID: sessionID, Data: &MessageCreatedData{Role: "user", Content: "Hello"}},
	}

	timeRangeAnnot := &annotations.Annotation{
		ID:        "ann-timerange",
		Type:      annotations.TypeComment,
		Key:       "note",
		SessionID: sessionID,
		Value:     annotations.AnnotationValue{Text: "Time range note"},
		Target: annotations.Target{
			Type:      annotations.TargetTimeRange,
			StartTime: baseTime.Add(500 * time.Millisecond),
			EndTime:   baseTime.Add(1500 * time.Millisecond),
		},
	}

	session := &AnnotatedSession{
		SessionID:   sessionID,
		Events:      testEvents,
		Timeline:    NewMediaTimeline(sessionID, testEvents, nil),
		Annotations: []*annotations.Annotation{timeRangeAnnot},
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(2 * time.Second),
			Duration:  2 * time.Second,
		},
	}

	exporter := NewSessionExporter(session, nil)
	timeline := exporter.buildJSONTimeline()

	// Should have 1 event + 1 annotation
	assert.Len(t, timeline.Events, 2)

	// Find the annotation event
	var annotEvent *JSONTimelineItem
	for i := range timeline.Events {
		if timeline.Events[i].Type == "annotation.comment" {
			annotEvent = &timeline.Events[i]
			break
		}
	}

	require.NotNil(t, annotEvent)
	assert.Equal(t, "note", annotEvent.Data["key"])
	assert.Equal(t, 1.0, annotEvent.Duration) // 1 second duration
}

func TestSessionExporter_FormatAnnotation_AllTypes(t *testing.T) {
	session := createTestAnnotatedSession(t)
	exporter := NewSessionExporter(session, nil)

	t.Run("flag annotation true", func(t *testing.T) {
		flagTrue := true
		annot := &annotations.Annotation{
			Type:  annotations.TypeFlag,
			Value: annotations.AnnotationValue{Flag: &flagTrue},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "Flagged", result)
	})

	t.Run("flag annotation false", func(t *testing.T) {
		flagFalse := false
		annot := &annotations.Annotation{
			Type:  annotations.TypeFlag,
			Value: annotations.AnnotationValue{Flag: &flagFalse},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "", result)
	})

	t.Run("flag annotation nil", func(t *testing.T) {
		annot := &annotations.Annotation{
			Type:  annotations.TypeFlag,
			Value: annotations.AnnotationValue{Flag: nil},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "", result)
	})

	t.Run("assertion passed", func(t *testing.T) {
		passed := true
		annot := &annotations.Annotation{
			Type:  annotations.TypeAssertion,
			Value: annotations.AnnotationValue{Passed: &passed},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "PASS", result)
	})

	t.Run("assertion failed", func(t *testing.T) {
		passed := false
		annot := &annotations.Annotation{
			Type:  annotations.TypeAssertion,
			Value: annotations.AnnotationValue{Passed: &passed, Message: "Value mismatch"},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "FAIL: Value mismatch", result)
	})

	t.Run("assertion nil", func(t *testing.T) {
		annot := &annotations.Annotation{
			Type:  annotations.TypeAssertion,
			Value: annotations.AnnotationValue{Passed: nil},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "", result)
	})

	t.Run("unknown annotation type", func(t *testing.T) {
		annot := &annotations.Annotation{
			Type:  annotations.AnnotationType("unknown"),
			Value: annotations.AnnotationValue{},
		}
		result := exporter.formatAnnotation(annot)
		assert.Equal(t, "", result)
	})
}

func TestGetAudioFormat_WithAudioTrack(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-session-audio"

	// Create events with audio data
	audioEvent := &Event{
		Type:      EventAudioInput,
		Timestamp: baseTime,
		SessionID: sessionID,
		Data: &AudioInputData{
			Actor: "user",
			Metadata: AudioMetadata{
				SampleRate: 16000,
				Channels:   2,
			},
		},
	}

	session := &AnnotatedSession{
		SessionID: sessionID,
		Events:    []*Event{audioEvent},
		Timeline:  NewMediaTimeline(sessionID, []*Event{audioEvent}, nil),
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(time.Second),
			Duration:  time.Second,
		},
	}

	config := &ExportConfig{}
	exporter := &SessionExporter{config: config, session: session}

	sampleRate, channels := exporter.getAudioFormat()

	// Should return values from the audio track
	assert.Equal(t, 16000, sampleRate)
	assert.Equal(t, 2, channels)
}

func TestExportSession_Success(t *testing.T) {
	session := createTestAnnotatedSession(t)
	outputPath := filepath.Join(t.TempDir(), "export.json")

	err := ExportSession(context.Background(), session, outputPath, ExportFormatJSON)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(outputPath)
	require.NoError(t, err)
}

func TestSessionExporter_Export_JSON_WriteError(t *testing.T) {
	session := createTestAnnotatedSession(t)

	config := &ExportConfig{
		Format:     ExportFormatJSON,
		OutputPath: "/nonexistent/directory/file.json", // Should fail to write
	}

	exporter := NewSessionExporter(session, config)
	err := exporter.Export(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write file")
}

func TestBuildJSONTimeline_AnnotationTimeFiltering(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-session-annot-filter"

	testEvents := []*Event{
		{Type: EventMessageCreated, Timestamp: baseTime, SessionID: sessionID, Data: &MessageCreatedData{Role: "user", Content: "1"}},
	}

	// Create annotation at 2 seconds
	annot := &annotations.Annotation{
		ID:        "ann-1",
		Type:      annotations.TypeComment,
		Key:       "note",
		SessionID: sessionID,
		Value:     annotations.AnnotationValue{Text: "Late annotation"},
		Target: annotations.Target{
			Type:      annotations.TargetTimeRange,
			StartTime: baseTime.Add(2 * time.Second),
			EndTime:   baseTime.Add(3 * time.Second),
		},
	}

	session := &AnnotatedSession{
		SessionID:   sessionID,
		Events:      testEvents,
		Timeline:    NewMediaTimeline(sessionID, testEvents, nil),
		Annotations: []*annotations.Annotation{annot},
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(3 * time.Second),
			Duration:  3 * time.Second,
		},
	}

	t.Run("annotation filtered by start time", func(t *testing.T) {
		config := &ExportConfig{
			StartTime: 2*time.Second + 500*time.Millisecond, // After annotation start
		}
		exporter := NewSessionExporter(session, config)
		timeline := exporter.buildJSONTimeline()

		// Annotation should be filtered out since it starts before StartTime
		var foundAnnot bool
		for _, e := range timeline.Events {
			if e.Type == "annotation.comment" {
				foundAnnot = true
			}
		}
		assert.False(t, foundAnnot)
	})

	t.Run("annotation filtered by end time", func(t *testing.T) {
		config := &ExportConfig{
			EndTime: time.Second, // Before annotation
		}
		exporter := NewSessionExporter(session, config)
		timeline := exporter.buildJSONTimeline()

		// Annotation should be filtered out
		var foundAnnot bool
		for _, e := range timeline.Events {
			if e.Type == "annotation.comment" {
				foundAnnot = true
			}
		}
		assert.False(t, foundAnnot)
	})
}

func TestBuildJSONTimeline_WithTracks(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-session-tracks"

	// Create audio events to populate tracks
	audioData := &AudioInputData{
		Actor:      "user",
		ChunkIndex: 0,
		Payload: BinaryPayload{
			StorageRef: "audio/chunk-001.pcm",
			Size:       3200,
		},
		Metadata: AudioMetadata{
			SampleRate: 16000,
			Channels:   1,
			DurationMs: 100,
		},
	}

	testEvents := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: baseTime,
			SessionID: sessionID,
			Data:      audioData,
		},
	}

	session := &AnnotatedSession{
		SessionID: sessionID,
		Events:    testEvents,
		Timeline:  NewMediaTimeline(sessionID, testEvents, nil),
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(time.Second),
			Duration:  time.Second,
		},
	}

	exporter := NewSessionExporter(session, nil)
	timeline := exporter.buildJSONTimeline()

	// Should have at least one track
	assert.NotEmpty(t, timeline.Tracks)

	// Find audio input track
	var foundAudioTrack bool
	for _, track := range timeline.Tracks {
		if track.Type == string(TrackAudioInput) {
			foundAudioTrack = true
			assert.NotEmpty(t, track.Segments)
		}
	}
	assert.True(t, foundAudioTrack, "expected to find audio input track")
}

func TestSessionExporter_Export_WAV_NoFFmpeg(t *testing.T) {
	session := createTestAnnotatedSession(t)
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	config := &ExportConfig{
		Format:     ExportFormatWAV,
		OutputPath: outputPath,
		FFmpegPath: "/nonexistent/ffmpeg", // Non-existent ffmpeg path
	}

	exporter := NewSessionExporter(session, config)
	err := exporter.Export(context.Background())

	// Should fail because ffmpeg is not found
	require.Error(t, err)
}

func TestSessionExporter_Export_MP4_NoFFmpeg(t *testing.T) {
	session := createTestAnnotatedSession(t)
	outputPath := filepath.Join(t.TempDir(), "output.mp4")

	config := &ExportConfig{
		Format:     ExportFormatMP4,
		OutputPath: outputPath,
		FFmpegPath: "/nonexistent/ffmpeg", // Non-existent ffmpeg path
	}

	exporter := NewSessionExporter(session, config)
	err := exporter.Export(context.Background())

	// Should fail because ffmpeg is not found
	require.Error(t, err)
}

func TestBuildAudioFFmpegArgs(t *testing.T) {
	session := createTestAnnotatedSession(t)

	t.Run("no inputs returns nil", func(t *testing.T) {
		config := &ExportConfig{Format: ExportFormatWAV}
		exporter := NewSessionExporter(session, config)
		args := exporter.buildAudioFFmpegArgs(nil, nil, "")
		assert.Nil(t, args)
	})

	t.Run("single input", func(t *testing.T) {
		config := &ExportConfig{Format: ExportFormatWAV, AudioMix: audioMixInput}
		exporter := NewSessionExporter(session, config)
		args := exporter.buildAudioFFmpegArgs([]string{"/tmp/input.raw"}, nil, "")

		assert.Contains(t, args, "-f")
		assert.Contains(t, args, "s16le")
		assert.Contains(t, args, "-map")
		assert.Contains(t, args, "0:a")
		assert.Contains(t, args, "pcm_s16le") // WAV codec
	})

	t.Run("stereo mix with two inputs", func(t *testing.T) {
		config := &ExportConfig{Format: ExportFormatWAV, AudioMix: audioMixStereo}
		exporter := NewSessionExporter(session, config)
		args := exporter.buildAudioFFmpegArgs([]string{"/tmp/input.raw"}, []string{"/tmp/output.raw"}, "")

		assert.Contains(t, args, "-filter_complex")
		// Should contain stereo pan
		found := false
		for _, arg := range args {
			if arg == "[0:a][1:a]amerge=inputs=2,pan=stereo|c0=c0|c1=c1[a]" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected stereo filter")
	})

	t.Run("mono mix with two inputs", func(t *testing.T) {
		config := &ExportConfig{Format: ExportFormatMP3, AudioMix: audioMixMono}
		exporter := NewSessionExporter(session, config)
		args := exporter.buildAudioFFmpegArgs([]string{"/tmp/input.raw"}, []string{"/tmp/output.raw"}, "")

		assert.Contains(t, args, "-filter_complex")
		// Should contain mono amix
		found := false
		for _, arg := range args {
			if arg == "[0:a][1:a]amix=inputs=2:duration=longest[a]" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected mono filter")
		assert.Contains(t, args, "libmp3lame") // MP3 codec
	})
}

func TestGenerateSubtitles(t *testing.T) {
	baseTime := time.Now()
	sessionID := "test-subtitles"

	t.Run("with transcriptions", func(t *testing.T) {
		events := []*Event{
			{
				Type:      EventAudioTranscription,
				Timestamp: baseTime,
				SessionID: sessionID,
				Data:      &AudioTranscriptionData{Text: "Hello world"},
			},
			{
				Type:      EventAudioTranscription,
				Timestamp: baseTime.Add(2 * time.Second),
				SessionID: sessionID,
				Data:      &AudioTranscriptionData{Text: "How are you?"},
			},
		}

		session := &AnnotatedSession{
			SessionID: sessionID,
			Events:    events,
			Timeline:  NewMediaTimeline(sessionID, events, nil),
			Metadata: SessionMetadata{
				StartTime: baseTime,
				EndTime:   baseTime.Add(5 * time.Second),
				Duration:  5 * time.Second,
			},
		}

		config := &ExportConfig{
			IncludeTranscriptions: true,
			IncludeAnnotations:    false,
		}
		exporter := NewSessionExporter(session, config)

		outputPath := filepath.Join(t.TempDir(), "subtitles.srt")
		err := exporter.generateSubtitles(outputPath)
		require.NoError(t, err)

		// Read and verify content
		content, err := os.ReadFile(outputPath)
		require.NoError(t, err)

		assert.Contains(t, string(content), "Hello world")
		assert.Contains(t, string(content), "How are you?")
	})

	t.Run("with annotations time range", func(t *testing.T) {
		score := 0.9
		annots := []*annotations.Annotation{
			{
				ID:        "ann-1",
				Type:      annotations.TypeScore,
				Key:       "quality",
				SessionID: sessionID,
				Value:     annotations.AnnotationValue{Score: &score},
				Target: annotations.Target{
					Type:      annotations.TargetTimeRange,
					StartTime: baseTime.Add(time.Second),
					EndTime:   baseTime.Add(3 * time.Second),
				},
			},
		}

		session := &AnnotatedSession{
			SessionID:   sessionID,
			Events:      []*Event{},
			Timeline:    NewMediaTimeline(sessionID, nil, nil),
			Annotations: annots,
			Metadata: SessionMetadata{
				StartTime: baseTime,
				EndTime:   baseTime.Add(5 * time.Second),
				Duration:  5 * time.Second,
			},
		}

		config := &ExportConfig{
			IncludeTranscriptions: false,
			IncludeAnnotations:    true,
		}
		exporter := NewSessionExporter(session, config)

		outputPath := filepath.Join(t.TempDir(), "subtitles.srt")
		err := exporter.generateSubtitles(outputPath)
		require.NoError(t, err)

		content, err := os.ReadFile(outputPath)
		require.NoError(t, err)

		assert.Contains(t, string(content), "[quality]")
		assert.Contains(t, string(content), "0.90")
	})

	t.Run("skips annotation with empty text", func(t *testing.T) {
		annots := []*annotations.Annotation{
			{
				ID:        "ann-1",
				Type:      annotations.TypeScore,
				Key:       "empty",
				SessionID: sessionID,
				Value:     annotations.AnnotationValue{Score: nil}, // Will format to empty
				Target: annotations.Target{
					Type: annotations.TargetSession,
				},
			},
		}

		session := &AnnotatedSession{
			SessionID:   sessionID,
			Events:      []*Event{},
			Timeline:    NewMediaTimeline(sessionID, nil, nil),
			Annotations: annots,
			Metadata: SessionMetadata{
				StartTime: baseTime,
				EndTime:   baseTime.Add(5 * time.Second),
				Duration:  5 * time.Second,
			},
		}

		config := &ExportConfig{
			IncludeTranscriptions: false,
			IncludeAnnotations:    true,
		}
		exporter := NewSessionExporter(session, config)

		outputPath := filepath.Join(t.TempDir(), "subtitles.srt")
		err := exporter.generateSubtitles(outputPath)
		require.NoError(t, err)

		content, err := os.ReadFile(outputPath)
		require.NoError(t, err)

		// Should be empty since annotation formats to empty string
		assert.NotContains(t, string(content), "[empty]")
	})

	t.Run("error on invalid path", func(t *testing.T) {
		session := &AnnotatedSession{
			SessionID: sessionID,
			Events:    []*Event{},
			Timeline:  NewMediaTimeline(sessionID, nil, nil),
			Metadata:  SessionMetadata{StartTime: baseTime},
		}
		exporter := NewSessionExporter(session, nil)

		err := exporter.generateSubtitles("/nonexistent/dir/subtitles.srt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create subtitle file")
	})

	t.Run("with non-transcription event skipped", func(t *testing.T) {
		events := []*Event{
			{
				Type:      EventMessageCreated,
				Timestamp: baseTime,
				SessionID: sessionID,
				Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
			},
		}

		session := &AnnotatedSession{
			SessionID: sessionID,
			Events:    events,
			Timeline:  NewMediaTimeline(sessionID, events, nil),
			Metadata: SessionMetadata{
				StartTime: baseTime,
				EndTime:   baseTime.Add(time.Second),
				Duration:  time.Second,
			},
		}

		config := &ExportConfig{IncludeTranscriptions: true}
		exporter := NewSessionExporter(session, config)

		outputPath := filepath.Join(t.TempDir(), "subtitles.srt")
		err := exporter.generateSubtitles(outputPath)
		require.NoError(t, err)

		content, err := os.ReadFile(outputPath)
		require.NoError(t, err)

		// Should be empty - MessageCreated is not a transcription
		assert.Empty(t, string(content))
	})
}

// Helper to create a test annotated session.
func createTestAnnotatedSession(t *testing.T) *AnnotatedSession {
	t.Helper()

	baseTime := time.Now()
	sessionID := "test-session"

	// Create events
	testEvents := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: baseTime,
			SessionID: sessionID,
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: baseTime.Add(time.Second),
			SessionID: sessionID,
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi there!"},
		},
	}

	// Create timeline
	timeline := NewMediaTimeline(sessionID, testEvents, nil)

	// Create annotation
	ann := &annotations.Annotation{
		ID:        "ann-1",
		Type:      annotations.TypeComment,
		Key:       "quality",
		SessionID: sessionID,
		Value:     annotations.AnnotationValue{Text: "Test annotation"},
		CreatedAt: baseTime,
		Target: annotations.Target{
			Type: annotations.TargetSession,
		},
	}

	return &AnnotatedSession{
		SessionID:   sessionID,
		Events:      testEvents,
		Timeline:    timeline,
		Annotations: []*annotations.Annotation{ann},
		Metadata: SessionMetadata{
			StartTime: baseTime,
			EndTime:   baseTime.Add(time.Second),
			Duration:  time.Second,
		},
	}
}
