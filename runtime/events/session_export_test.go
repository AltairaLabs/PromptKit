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
