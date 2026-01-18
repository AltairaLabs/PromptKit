package events

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

func TestMediaTimeline_BasicConstruction(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &AudioInputData{
				Actor:      "user",
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: make([]byte, 4800), MIMEType: "audio/pcm"},
			},
		},
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioInputData{
				Actor:      "user",
				ChunkIndex: 1,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: make([]byte, 4800), MIMEType: "audio/pcm"},
			},
		},
		{
			Type:      EventAudioOutput,
			Timestamp: sessionStart.Add(200 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioOutputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 150},
				Payload:    BinaryPayload{InlineData: make([]byte, 7200), MIMEType: "audio/pcm"},
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	if timeline.SessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got %s", timeline.SessionID)
	}

	if !timeline.HasTrack(TrackAudioInput) {
		t.Error("Expected audio input track")
	}

	if !timeline.HasTrack(TrackAudioOutput) {
		t.Error("Expected audio output track")
	}

	if timeline.HasTrack(TrackVideo) {
		t.Error("Should not have video track")
	}

	// Check audio input track
	inputTrack := timeline.GetTrack(TrackAudioInput)
	if inputTrack == nil {
		t.Fatal("Audio input track is nil")
	}
	if len(inputTrack.Segments) != 2 {
		t.Errorf("Expected 2 input segments, got %d", len(inputTrack.Segments))
	}
	if inputTrack.TotalDuration != 200*time.Millisecond {
		t.Errorf("Expected 200ms total duration, got %v", inputTrack.TotalDuration)
	}

	// Check audio output track
	outputTrack := timeline.GetTrack(TrackAudioOutput)
	if outputTrack == nil {
		t.Fatal("Audio output track is nil")
	}
	if len(outputTrack.Segments) != 1 {
		t.Errorf("Expected 1 output segment, got %d", len(outputTrack.Segments))
	}
}

func TestMediaTrack_OffsetInSegment(t *testing.T) {
	track := &MediaTrack{
		Type: TrackAudioInput,
		Segments: []*MediaSegment{
			{StartTime: 0, Duration: 100 * time.Millisecond},
			{StartTime: 100 * time.Millisecond, Duration: 150 * time.Millisecond},
			{StartTime: 250 * time.Millisecond, Duration: 200 * time.Millisecond},
		},
		TotalDuration: 450 * time.Millisecond,
	}

	tests := []struct {
		offset         time.Duration
		expectedSegIdx int
		expectedOffset time.Duration
		expectNil      bool
	}{
		{0, 0, 0, false},
		{50 * time.Millisecond, 0, 50 * time.Millisecond, false},
		{100 * time.Millisecond, 1, 0, false},
		{200 * time.Millisecond, 1, 100 * time.Millisecond, false},
		{300 * time.Millisecond, 2, 50 * time.Millisecond, false},
		{500 * time.Millisecond, 0, 0, true}, // Past end
	}

	for i, tt := range tests {
		seg, offset := track.OffsetInSegment(tt.offset)
		if tt.expectNil {
			if seg != nil {
				t.Errorf("Test %d: expected nil segment for offset %v", i, tt.offset)
			}
			continue
		}
		if seg == nil {
			t.Errorf("Test %d: unexpected nil segment for offset %v", i, tt.offset)
			continue
		}
		if seg != track.Segments[tt.expectedSegIdx] {
			t.Errorf("Test %d: wrong segment index", i)
		}
		if offset != tt.expectedOffset {
			t.Errorf("Test %d: expected offset %v, got %v", i, tt.expectedOffset, offset)
		}
	}
}

func TestTrackReader_Read(t *testing.T) {
	// Create a timeline with inline data
	sessionStart := time.Now()
	chunk1 := make([]byte, 100)
	chunk2 := make([]byte, 100)
	for i := range chunk1 {
		chunk1[i] = 0x01
		chunk2[i] = 0x02
	}

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &AudioInputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: chunk1, MIMEType: "audio/pcm", Size: 100},
			},
		},
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioInputData{
				ChunkIndex: 1,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: chunk2, MIMEType: "audio/pcm", Size: 100},
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	reader, err := timeline.NewTrackReader(TrackAudioInput)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// Read all data
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(data) != 200 {
		t.Errorf("Expected 200 bytes, got %d", len(data))
	}

	// Verify data from first chunk
	for i := 0; i < 100; i++ {
		if data[i] != 0x01 {
			t.Errorf("Byte %d should be 0x01, got 0x%02x", i, data[i])
			break
		}
	}

	// Verify data from second chunk
	for i := 100; i < 200; i++ {
		if data[i] != 0x02 {
			t.Errorf("Byte %d should be 0x02, got 0x%02x", i, data[i])
			break
		}
	}
}

func TestTrackReader_Seek(t *testing.T) {
	sessionStart := time.Now()
	chunk := make([]byte, 1000)

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &AudioInputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 1000},
				Payload:    BinaryPayload{InlineData: chunk, MIMEType: "audio/pcm", Size: 1000},
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	reader, err := timeline.NewTrackReader(TrackAudioInput)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// Seek to middle
	if err := reader.Seek(500 * time.Millisecond); err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	// Position should be updated
	if reader.Position() != 500*time.Millisecond {
		t.Errorf("Expected position 500ms, got %v", reader.Position())
	}
}

func TestTimelineBuilder(t *testing.T) {
	builder := NewTimelineBuilder("test-session", nil)

	sessionStart := time.Now()

	// Add events out of order
	builder.AddEvent(&Event{
		Type:      EventAudioInput,
		Timestamp: sessionStart.Add(100 * time.Millisecond),
		SessionID: "test-session",
		Data: &AudioInputData{
			ChunkIndex: 1,
			Metadata:   AudioMetadata{DurationMs: 100},
			Payload:    BinaryPayload{InlineData: make([]byte, 100)},
		},
	})

	builder.AddEvent(&Event{
		Type:      EventAudioInput,
		Timestamp: sessionStart,
		SessionID: "test-session",
		Data: &AudioInputData{
			ChunkIndex: 0,
			Metadata:   AudioMetadata{DurationMs: 100},
			Payload:    BinaryPayload{InlineData: make([]byte, 100)},
		},
	})

	timeline := builder.Build()

	if len(timeline.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(timeline.Events))
	}

	// Events should be sorted by timestamp
	if timeline.Events[0].Timestamp.After(timeline.Events[1].Timestamp) {
		t.Error("Events should be sorted by timestamp")
	}
}

func TestMixedAudioReader(t *testing.T) {
	sessionStart := time.Now()
	inputChunk := make([]byte, 100)
	outputChunk := make([]byte, 100)

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &AudioInputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: inputChunk, MIMEType: "audio/pcm", Size: 100},
			},
		},
		{
			Type:      EventAudioOutput,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &AudioOutputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: outputChunk, MIMEType: "audio/pcm", Size: 100},
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	reader, err := timeline.NewMixedAudioReader()
	if err != nil {
		t.Fatalf("Failed to create mixed reader: %v", err)
	}
	defer reader.Close()

	if reader.SampleRate() != 24000 {
		t.Errorf("Expected sample rate 24000, got %d", reader.SampleRate())
	}

	if reader.Channels() != 1 {
		t.Errorf("Expected 1 channel, got %d", reader.Channels())
	}
}

func TestLoadMediaTimeline(t *testing.T) {
	// Create a temporary directory for the event store
	tempDir, err := os.MkdirTemp("", "media-timeline-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create event store
	store, err := NewFileEventStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Add some events
	ctx := context.Background()
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &AudioInputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: make([]byte, 100), MIMEType: "audio/pcm"},
			},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data: &MessageCreatedData{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	for _, event := range events {
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Failed to append event: %v", err)
		}
	}

	// Load the timeline
	timeline, err := LoadMediaTimeline(ctx, store, nil, "test-session")
	if err != nil {
		t.Fatalf("Failed to load timeline: %v", err)
	}

	if timeline.SessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got %s", timeline.SessionID)
	}

	if len(timeline.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(timeline.Events))
	}

	if !timeline.HasTrack(TrackAudioInput) {
		t.Error("Expected audio input track")
	}
}

func TestVideoFrameExtraction(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventVideoFrame,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data: &VideoFrameData{
				FrameIndex:  0,
				TimestampMs: 0,
				IsKeyframe:  true,
				Metadata:    VideoMetadata{Width: 1920, Height: 1080, Encoding: "h264"},
				Payload:     BinaryPayload{InlineData: make([]byte, 10000), MIMEType: "video/h264"},
			},
		},
		{
			Type:      EventVideoFrame,
			Timestamp: sessionStart.Add(33 * time.Millisecond),
			SessionID: "test-session",
			Data: &VideoFrameData{
				FrameIndex:  1,
				TimestampMs: 33,
				IsKeyframe:  false,
				Metadata:    VideoMetadata{Width: 1920, Height: 1080, Encoding: "h264"},
				Payload:     BinaryPayload{InlineData: make([]byte, 5000), MIMEType: "video/h264"},
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	if !timeline.HasTrack(TrackVideo) {
		t.Error("Expected video track")
	}

	videoTrack := timeline.GetTrack(TrackVideo)
	if videoTrack == nil {
		t.Fatal("Video track is nil")
	}

	if len(videoTrack.Segments) != 2 {
		t.Errorf("Expected 2 video segments, got %d", len(videoTrack.Segments))
	}

	// First segment should be keyframe
	if videoTrack.Segments[0].ChunkIndex != 0 {
		t.Errorf("First segment should have frame index 0")
	}
}

func TestMediaTrack_ExportToWAV(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "wav-test",
			Data: &AudioInputData{
				Actor:      "user",
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 16000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: make([]byte, 3200), MIMEType: "audio/pcm", Size: 3200},
			},
		},
		{
			Type:      EventAudioOutput,
			Timestamp: sessionStart.Add(200 * time.Millisecond),
			SessionID: "wav-test",
			Data: &AudioOutputData{
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: make([]byte, 4800), MIMEType: "audio/pcm", Size: 4800},
			},
		},
	}

	timeline := NewMediaTimeline("wav-test", events, nil)

	// Test ExportAudioToWAV for input track
	inputPath := t.TempDir() + "/input.wav"
	if err := timeline.ExportAudioToWAV(TrackAudioInput, inputPath); err != nil {
		t.Fatalf("Failed to export input audio: %v", err)
	}

	// Verify WAV file was created
	inputData, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("Failed to read WAV file: %v", err)
	}

	// Check WAV header
	if string(inputData[0:4]) != "RIFF" {
		t.Error("WAV file missing RIFF header")
	}
	if string(inputData[8:12]) != "WAVE" {
		t.Error("WAV file missing WAVE format")
	}
	if string(inputData[12:16]) != "fmt " {
		t.Error("WAV file missing fmt chunk")
	}

	// Test ExportAudioToWAV for output track
	outputPath := t.TempDir() + "/output.wav"
	if err := timeline.ExportAudioToWAV(TrackAudioOutput, outputPath); err != nil {
		t.Fatalf("Failed to export output audio: %v", err)
	}

	outputData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output WAV file: %v", err)
	}
	if len(outputData) < 44 {
		t.Error("Output WAV file too small")
	}
}

func TestMediaTrack_ExportToWAV_NonAudioTrack(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventVideoFrame,
			Timestamp: sessionStart,
			SessionID: "video-test",
			Data: &VideoFrameData{
				FrameIndex: 0,
				Metadata:   VideoMetadata{Width: 640, Height: 480},
				Payload:    BinaryPayload{InlineData: make([]byte, 1000), MIMEType: "video/h264"},
			},
		},
	}

	timeline := NewMediaTimeline("video-test", events, nil)

	// Should fail for video track
	videoPath := t.TempDir() + "/video.wav"
	err := timeline.ExportAudioToWAV(TrackVideo, videoPath)
	if err == nil {
		t.Error("Expected error when exporting video track to WAV")
	}
}

func TestMediaTrack_ExportToWAV_NoSegments(t *testing.T) {
	// Create a track with no segments
	track := &MediaTrack{
		Type:     TrackAudioInput,
		Segments: []*MediaSegment{},
	}

	tmpPath := t.TempDir() + "/empty.wav"
	err := track.ExportToWAV(tmpPath, nil)
	if err == nil {
		t.Error("Expected error for empty track")
	}
}

func TestMediaTrack_ExportToWAV_NoFormat(t *testing.T) {
	// Track with segments but no format metadata - should use defaults
	track := &MediaTrack{
		Type: TrackAudioInput,
		Segments: []*MediaSegment{
			{
				Payload:  &BinaryPayload{InlineData: make([]byte, 1000)},
				Metadata: nil, // No metadata
			},
		},
		Format: nil, // No format
	}

	tmpPath := t.TempDir() + "/noformat.wav"
	err := track.ExportToWAV(tmpPath, nil)
	if err != nil {
		t.Errorf("Should handle missing format: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(tmpPath); os.IsNotExist(err) {
		t.Error("WAV file was not created")
	}
}

func TestMediaTimeline_ExportAudioToWAV_TrackNotFound(t *testing.T) {
	timeline := NewMediaTimeline("empty", []*Event{}, nil)

	err := timeline.ExportAudioToWAV(TrackAudioInput, "/tmp/notfound.wav")
	if err == nil {
		t.Error("Expected error for missing track")
	}
}

func TestMixedAudioReader_SeekAndPosition(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart,
			SessionID: "mix-test",
			Data: &AudioInputData{
				Actor:      "user",
				ChunkIndex: 0,
				Metadata:   AudioMetadata{SampleRate: 24000, Channels: 1, DurationMs: 100},
				Payload:    BinaryPayload{InlineData: make([]byte, 4800), MIMEType: "audio/pcm"},
			},
		},
	}

	timeline := NewMediaTimeline("mix-test", events, nil)

	reader, err := timeline.NewMixedAudioReader()
	if err != nil {
		t.Fatalf("Failed to create mixed reader: %v", err)
	}
	defer reader.Close()

	// Test Seek
	if err := reader.Seek(50 * time.Millisecond); err != nil {
		t.Errorf("Seek failed: %v", err)
	}

	// Test Position
	if reader.Position() != 50*time.Millisecond {
		t.Errorf("Expected position 50ms, got %v", reader.Position())
	}
}
