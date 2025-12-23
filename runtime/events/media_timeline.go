// Package events provides event storage and replay for session recording.
package events

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

// TrackType identifies the type of media track.
type TrackType string

const (
	// TrackAudioInput represents user/environment audio input.
	TrackAudioInput TrackType = "audio_input"
	// TrackAudioOutput represents agent audio output.
	TrackAudioOutput TrackType = "audio_output"
	// TrackVideo represents video frames.
	TrackVideo TrackType = "video"
)

// MediaSegment represents a continuous segment of media data.
type MediaSegment struct {
	// StartTime is when this segment starts relative to session start.
	StartTime time.Duration
	// Duration is how long this segment lasts.
	Duration time.Duration
	// Payload contains the media data or reference.
	Payload *BinaryPayload
	// Metadata contains format information.
	Metadata interface{} // AudioMetadata or VideoMetadata
	// EventIndex is the index of the source event.
	EventIndex int
	// ChunkIndex is the original chunk sequence number.
	ChunkIndex int
}

// MediaTrack represents a single track of media (e.g., audio input, audio output).
type MediaTrack struct {
	// Type identifies the track type.
	Type TrackType
	// Segments are the ordered media segments in this track.
	Segments []*MediaSegment
	// TotalDuration is the total duration of all segments.
	TotalDuration time.Duration
	// Format contains track-level format information.
	Format interface{} // AudioMetadata or VideoMetadata
}

// OffsetInSegment returns the segment containing the given offset and the position within it.
// Returns nil if the offset is beyond the track duration.
func (t *MediaTrack) OffsetInSegment(offset time.Duration) (*MediaSegment, time.Duration) {
	var accumulated time.Duration
	for _, seg := range t.Segments {
		if offset < accumulated+seg.Duration {
			return seg, offset - accumulated
		}
		accumulated += seg.Duration
	}
	return nil, 0
}

// MediaTimeline represents a complete media timeline for a session.
// It organizes audio/video data from events into seekable tracks.
type MediaTimeline struct {
	// SessionID is the session this timeline belongs to.
	SessionID string
	// SessionStart is when the session started.
	SessionStart time.Time
	// SessionEnd is when the session ended.
	SessionEnd time.Time
	// Tracks contains all media tracks indexed by type.
	Tracks map[TrackType]*MediaTrack
	// Events are all the source events.
	Events []*Event
	// BlobStore for loading media data.
	blobStore BlobStore
}

// NewMediaTimeline creates a new media timeline from session events.
func NewMediaTimeline(sessionID string, events []*Event, blobStore BlobStore) *MediaTimeline {
	mt := &MediaTimeline{
		SessionID: sessionID,
		Tracks:    make(map[TrackType]*MediaTrack),
		Events:    events,
		blobStore: blobStore,
	}

	if len(events) > 0 {
		mt.SessionStart = events[0].Timestamp
		mt.SessionEnd = events[len(events)-1].Timestamp
	}

	mt.buildTracks()
	return mt
}

// buildTracks extracts media segments from events and organizes them into tracks.
func (mt *MediaTimeline) buildTracks() {
	// Collect audio input segments
	audioInputSegments := mt.extractAudioSegments(EventAudioInput, TrackAudioInput)
	if len(audioInputSegments) > 0 {
		mt.Tracks[TrackAudioInput] = mt.buildAudioTrack(TrackAudioInput, audioInputSegments)
	}

	// Collect audio output segments
	audioOutputSegments := mt.extractAudioSegments(EventAudioOutput, TrackAudioOutput)
	if len(audioOutputSegments) > 0 {
		mt.Tracks[TrackAudioOutput] = mt.buildAudioTrack(TrackAudioOutput, audioOutputSegments)
	}

	// Collect video frames
	videoSegments := mt.extractVideoSegments()
	if len(videoSegments) > 0 {
		mt.Tracks[TrackVideo] = mt.buildVideoTrack(videoSegments)
	}
}

// extractAudioSegments extracts audio segments of the specified event type.
func (mt *MediaTimeline) extractAudioSegments(eventType EventType, _ TrackType) []*MediaSegment {
	var segments []*MediaSegment

	for i, event := range mt.Events {
		if event.Type != eventType {
			continue
		}

		var payload *BinaryPayload
		var metadata AudioMetadata
		var chunkIndex int

		switch data := event.Data.(type) {
		case *AudioInputData:
			payload = &data.Payload
			metadata = data.Metadata
			chunkIndex = data.ChunkIndex
		case *AudioOutputData:
			payload = &data.Payload
			metadata = data.Metadata
			chunkIndex = data.ChunkIndex
		default:
			continue
		}

		segment := &MediaSegment{
			StartTime:  event.Timestamp.Sub(mt.SessionStart),
			Duration:   time.Duration(metadata.DurationMs) * time.Millisecond,
			Payload:    payload,
			Metadata:   metadata,
			EventIndex: i,
			ChunkIndex: chunkIndex,
		}
		segments = append(segments, segment)
	}

	// Sort by chunk index for proper ordering
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].ChunkIndex < segments[j].ChunkIndex
	})

	return segments
}

// extractVideoSegments extracts video frame segments.
func (mt *MediaTimeline) extractVideoSegments() []*MediaSegment {
	var segments []*MediaSegment

	for i, event := range mt.Events {
		if event.Type != EventVideoFrame {
			continue
		}

		data, ok := event.Data.(*VideoFrameData)
		if !ok {
			continue
		}

		segment := &MediaSegment{
			StartTime:  event.Timestamp.Sub(mt.SessionStart),
			Duration:   time.Duration(data.Metadata.DurationMs) * time.Millisecond,
			Payload:    &data.Payload,
			Metadata:   data.Metadata,
			EventIndex: i,
			ChunkIndex: int(data.FrameIndex),
		}
		segments = append(segments, segment)
	}

	// Sort by frame index
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].ChunkIndex < segments[j].ChunkIndex
	})

	return segments
}

// buildAudioTrack creates a track from audio segments.
func (mt *MediaTimeline) buildAudioTrack(trackType TrackType, segments []*MediaSegment) *MediaTrack {
	track := &MediaTrack{
		Type:     trackType,
		Segments: segments,
	}

	// Calculate total duration
	for _, seg := range segments {
		track.TotalDuration += seg.Duration
	}

	// Use first segment's metadata as track format
	if len(segments) > 0 {
		track.Format = segments[0].Metadata
	}

	return track
}

// buildVideoTrack creates a track from video segments.
func (mt *MediaTimeline) buildVideoTrack(segments []*MediaSegment) *MediaTrack {
	track := &MediaTrack{
		Type:     TrackVideo,
		Segments: segments,
	}

	// For video, duration is from first to last frame
	if len(segments) > 1 {
		track.TotalDuration = segments[len(segments)-1].StartTime - segments[0].StartTime
	}

	// Use first segment's metadata as track format
	if len(segments) > 0 {
		track.Format = segments[0].Metadata
	}

	return track
}

// Duration returns the total session duration.
func (mt *MediaTimeline) Duration() time.Duration {
	return mt.SessionEnd.Sub(mt.SessionStart)
}

// HasTrack returns true if the timeline has the specified track type.
func (mt *MediaTimeline) HasTrack(trackType TrackType) bool {
	_, ok := mt.Tracks[trackType]
	return ok
}

// GetTrack returns the track of the specified type, or nil if not present.
func (mt *MediaTimeline) GetTrack(trackType TrackType) *MediaTrack {
	return mt.Tracks[trackType]
}

// TrackReader provides a reader interface for a media track.
type TrackReader struct {
	track     *MediaTrack
	blobStore BlobStore
	position  time.Duration
	segIndex  int
	segOffset int64 // byte offset within current segment
	segData   []byte
}

// NewTrackReader creates a reader for the specified track.
func (mt *MediaTimeline) NewTrackReader(trackType TrackType) (*TrackReader, error) {
	track := mt.Tracks[trackType]
	if track == nil {
		return nil, fmt.Errorf("track %s not found", trackType)
	}

	return &TrackReader{
		track:     track,
		blobStore: mt.blobStore,
	}, nil
}

// Read implements io.Reader for streaming track data.
func (r *TrackReader) Read(p []byte) (n int, err error) {
	if r.segIndex >= len(r.track.Segments) {
		return 0, io.EOF
	}

	// Load segment data if not already loaded
	if r.segData == nil {
		seg := r.track.Segments[r.segIndex]
		if seg.Payload.InlineData != nil {
			r.segData = seg.Payload.InlineData
		} else if r.blobStore != nil {
			r.segData, err = r.blobStore.Load(context.Background(), seg.Payload.StorageRef)
			if err != nil {
				return 0, fmt.Errorf("load segment data: %w", err)
			}
		} else {
			return 0, fmt.Errorf("no data available for segment %d", r.segIndex)
		}
		r.segOffset = 0
	}

	// Read from current segment
	remaining := len(r.segData) - int(r.segOffset)
	if remaining <= 0 {
		// Move to next segment
		r.segData = nil
		r.segIndex++
		r.segOffset = 0
		// Update position
		if r.segIndex < len(r.track.Segments) {
			r.position = r.track.Segments[r.segIndex].StartTime
		}
		return r.Read(p)
	}

	n = copy(p, r.segData[r.segOffset:])
	r.segOffset += int64(n)

	return n, nil
}

// Seek implements io.Seeker for random access.
func (r *TrackReader) Seek(offset time.Duration) error {
	r.position = offset
	r.segData = nil

	// Find the segment containing this offset
	var accumulated time.Duration
	for i, seg := range r.track.Segments {
		if offset < accumulated+seg.Duration {
			r.segIndex = i
			// Calculate byte offset within segment (assuming constant bitrate)
			segDuration := seg.Duration
			if segDuration > 0 {
				fraction := float64(offset-accumulated) / float64(segDuration)
				r.segOffset = int64(fraction * float64(seg.Payload.Size))
			}
			return nil
		}
		accumulated += seg.Duration
	}

	// Past the end
	r.segIndex = len(r.track.Segments)
	return nil
}

// Position returns the current playback position.
func (r *TrackReader) Position() time.Duration {
	return r.position
}

// Close releases resources.
func (r *TrackReader) Close() error {
	r.segData = nil
	return nil
}

// MixedAudioReader provides a reader that mixes input and output audio tracks.
type MixedAudioReader struct {
	inputReader  *TrackReader
	outputReader *TrackReader
	position     time.Duration
	sampleRate   int
	channels     int
}

// NewMixedAudioReader creates a reader that mixes both audio tracks.
func (mt *MediaTimeline) NewMixedAudioReader() (*MixedAudioReader, error) {
	var inputReader, outputReader *TrackReader
	var err error

	if mt.HasTrack(TrackAudioInput) {
		inputReader, err = mt.NewTrackReader(TrackAudioInput)
		if err != nil {
			return nil, fmt.Errorf("create input reader: %w", err)
		}
	}

	if mt.HasTrack(TrackAudioOutput) {
		outputReader, err = mt.NewTrackReader(TrackAudioOutput)
		if err != nil {
			return nil, fmt.Errorf("create output reader: %w", err)
		}
	}

	if inputReader == nil && outputReader == nil {
		return nil, fmt.Errorf("no audio tracks available")
	}

	// Get sample rate from available track
	sampleRate := 24000 // default
	channels := 1
	if inputReader != nil {
		if meta, ok := inputReader.track.Format.(AudioMetadata); ok {
			sampleRate = meta.SampleRate
			channels = meta.Channels
		}
	} else if outputReader != nil {
		if meta, ok := outputReader.track.Format.(AudioMetadata); ok {
			sampleRate = meta.SampleRate
			channels = meta.Channels
		}
	}

	return &MixedAudioReader{
		inputReader:  inputReader,
		outputReader: outputReader,
		sampleRate:   sampleRate,
		channels:     channels,
	}, nil
}

// SampleRate returns the audio sample rate.
func (r *MixedAudioReader) SampleRate() int {
	return r.sampleRate
}

// Channels returns the number of audio channels.
func (r *MixedAudioReader) Channels() int {
	return r.channels
}

// Seek moves both readers to the specified position.
func (r *MixedAudioReader) Seek(offset time.Duration) error {
	r.position = offset
	if r.inputReader != nil {
		if err := r.inputReader.Seek(offset); err != nil {
			return err
		}
	}
	if r.outputReader != nil {
		if err := r.outputReader.Seek(offset); err != nil {
			return err
		}
	}
	return nil
}

// Position returns the current playback position.
func (r *MixedAudioReader) Position() time.Duration {
	return r.position
}

// Close releases resources.
func (r *MixedAudioReader) Close() error {
	if r.inputReader != nil {
		_ = r.inputReader.Close() // Error ignored during cleanup
	}
	if r.outputReader != nil {
		_ = r.outputReader.Close() // Error ignored during cleanup
	}
	return nil
}

// TimelineBuilder helps build a media timeline incrementally.
type TimelineBuilder struct {
	sessionID string
	events    []*Event
	blobStore BlobStore
}

// NewTimelineBuilder creates a new timeline builder.
func NewTimelineBuilder(sessionID string, blobStore BlobStore) *TimelineBuilder {
	return &TimelineBuilder{
		sessionID: sessionID,
		blobStore: blobStore,
	}
}

// AddEvent adds an event to the timeline.
func (b *TimelineBuilder) AddEvent(event *Event) {
	b.events = append(b.events, event)
}

// Build creates the final MediaTimeline.
func (b *TimelineBuilder) Build() *MediaTimeline {
	// Sort events by timestamp
	sort.Slice(b.events, func(i, j int) bool {
		return b.events[i].Timestamp.Before(b.events[j].Timestamp)
	})
	return NewMediaTimeline(b.sessionID, b.events, b.blobStore)
}

// LoadMediaTimeline loads a complete media timeline from storage.
func LoadMediaTimeline(
	ctx context.Context,
	store EventStore,
	blobStore BlobStore,
	sessionID string,
) (*MediaTimeline, error) {
	events, err := store.Query(ctx, &EventFilter{SessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	return NewMediaTimeline(sessionID, events, blobStore), nil
}

// WAV file constants.
const (
	wavHeaderSize     = 44
	wavAudioFormat    = 1 // PCM
	wavBitsPerByte    = 8
	wavDefaultBits    = 16
	wavFilePerms      = 0600
	wavSubchunk1Size  = 16
	defaultSampleRate = 24000
	defaultChannels   = 1
)

// ExportToWAV exports the audio track to a WAV file.
// The blobStore is used to load segment data that references external storage.
func (t *MediaTrack) ExportToWAV(path string, blobStore BlobStore) error {
	if err := t.validateAudioTrack(); err != nil {
		return err
	}

	sampleRate, channels := t.getAudioParams()
	audioData, err := t.collectAudioData(blobStore)
	if err != nil {
		return err
	}

	wavData := createWAVFile(audioData, sampleRate, channels, wavDefaultBits)
	if err := os.WriteFile(path, wavData, wavFilePerms); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// validateAudioTrack checks that the track is a valid audio track for export.
func (t *MediaTrack) validateAudioTrack() error {
	if t.Type != TrackAudioInput && t.Type != TrackAudioOutput {
		return fmt.Errorf("can only export audio tracks to WAV, got %s", t.Type)
	}
	if len(t.Segments) == 0 {
		return fmt.Errorf("no segments to export")
	}
	return nil
}

// getAudioParams returns the sample rate and channels for the track.
func (t *MediaTrack) getAudioParams() (sampleRate, channels int) {
	meta, ok := t.Format.(AudioMetadata)
	if !ok {
		return defaultSampleRate, defaultChannels
	}

	sampleRate = meta.SampleRate
	if sampleRate == 0 {
		sampleRate = defaultSampleRate
	}
	channels = meta.Channels
	if channels == 0 {
		channels = defaultChannels
	}
	return sampleRate, channels
}

// collectAudioData gathers all audio data from track segments.
func (t *MediaTrack) collectAudioData(blobStore BlobStore) ([]byte, error) {
	var audioData []byte
	for _, seg := range t.Segments {
		data, err := t.loadSegmentData(*seg, blobStore)
		if err != nil {
			return nil, err
		}
		audioData = append(audioData, data...)
	}

	if len(audioData) == 0 {
		return nil, fmt.Errorf("no audio data to export")
	}
	return audioData, nil
}

// loadSegmentData loads data from a single segment.
func (t *MediaTrack) loadSegmentData(seg MediaSegment, blobStore BlobStore) ([]byte, error) {
	if seg.Payload.InlineData != nil {
		return seg.Payload.InlineData, nil
	}
	if blobStore != nil && seg.Payload.StorageRef != "" {
		data, err := blobStore.Load(context.Background(), seg.Payload.StorageRef)
		if err != nil {
			return nil, fmt.Errorf("load segment %d: %w", seg.ChunkIndex, err)
		}
		return data, nil
	}
	return nil, nil // Skip segments with no data
}

// createWAVFile creates a WAV file from raw PCM data.
//
//nolint:gosec // Integer conversions are safe for audio parameters (bounded by format constraints)
func createWAVFile(pcmData []byte, sampleRate, channels, bitsPerSample int) []byte {
	dataSize := len(pcmData)
	byteRate := sampleRate * channels * bitsPerSample / wavBitsPerByte
	blockAlign := channels * bitsPerSample / wavBitsPerByte

	// Create buffer for header + data
	wav := make([]byte, wavHeaderSize+dataSize)

	// RIFF header
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(36+dataSize)) //nolint:mnd // WAV format constant
	copy(wav[8:12], "WAVE")

	// fmt subchunk
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], wavSubchunk1Size)
	binary.LittleEndian.PutUint16(wav[20:22], wavAudioFormat)
	binary.LittleEndian.PutUint16(wav[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(wav[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(wav[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(wav[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(wav[34:36], uint16(bitsPerSample))

	// data subchunk
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(dataSize))
	copy(wav[wavHeaderSize:], pcmData)

	return wav
}

// ExportAudioToWAV is a convenience method to export a specific audio track to WAV.
func (mt *MediaTimeline) ExportAudioToWAV(trackType TrackType, path string) error {
	track := mt.GetTrack(trackType)
	if track == nil {
		return fmt.Errorf("track %s not found", trackType)
	}
	return track.ExportToWAV(path, mt.blobStore)
}
