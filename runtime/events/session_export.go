// Package events provides event storage and replay for session recording.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
)

// ExportFormat specifies the output format for session export.
type ExportFormat string

const (
	// ExportFormatMP4 exports as MP4 video with H.264.
	ExportFormatMP4 ExportFormat = "mp4"
	// ExportFormatWebM exports as WebM video with VP9.
	ExportFormatWebM ExportFormat = "webm"
	// ExportFormatWAV exports audio only as WAV.
	ExportFormatWAV ExportFormat = "wav"
	// ExportFormatMP3 exports audio only as MP3.
	ExportFormatMP3 ExportFormat = "mp3"
	// ExportFormatJSON exports as JSON timeline.
	ExportFormatJSON ExportFormat = "json"
)

// Audio mix mode constants.
const (
	audioMixStereo = "stereo"
	audioMixMono   = "mono"
	audioMixOutput = "output"
	audioMixInput  = "input"
)

// Video defaults.
const (
	defaultVideoWidth  = 1280
	defaultVideoHeight = 720
	defaultFontSize    = 24
	defaultFFmpeg      = "ffmpeg"
	subtitleDuration   = 3 * time.Second
	ffmpegFilterComplex = "-filter_complex"
)

// ExportConfig configures session export behavior.
type ExportConfig struct {
	// Format is the output format.
	Format ExportFormat

	// OutputPath is the path to write the output file.
	OutputPath string

	// IncludeAnnotations when true, overlays annotations on video output.
	IncludeAnnotations bool

	// IncludeEvents when true, overlays events on video output.
	IncludeEvents bool

	// IncludeTranscriptions when true, overlays transcriptions on video output.
	IncludeTranscriptions bool

	// VideoWidth is the output video width (default: 1280).
	VideoWidth int

	// VideoHeight is the output video height (default: 720).
	VideoHeight int

	// FontSize is the font size for overlays (default: 24).
	FontSize int

	// AudioMix specifies how to mix audio tracks.
	// "stereo" = input on left, output on right
	// "mono" = mix both to mono
	// "output" = output audio only
	// "input" = input audio only
	AudioMix string

	// FFmpegPath is the path to ffmpeg binary (default: "ffmpeg").
	FFmpegPath string

	// StartTime is the start position for export (default: 0).
	StartTime time.Duration

	// EndTime is the end position for export (default: full duration).
	EndTime time.Duration

	// OnProgress is called with progress updates (0.0 to 1.0).
	OnProgress func(progress float64)
}

// DefaultExportConfig returns sensible defaults for export.
func DefaultExportConfig(outputPath string) *ExportConfig {
	return &ExportConfig{
		Format:                ExportFormatMP4,
		OutputPath:            outputPath,
		IncludeAnnotations:    true,
		IncludeEvents:         false,
		IncludeTranscriptions: true,
		VideoWidth:            defaultVideoWidth,
		VideoHeight:           defaultVideoHeight,
		FontSize:              defaultFontSize,
		AudioMix:              audioMixStereo,
		FFmpegPath:            defaultFFmpeg,
	}
}

// SessionExporter exports annotated sessions to various formats.
type SessionExporter struct {
	session *AnnotatedSession
	config  *ExportConfig
}

// NewSessionExporter creates a new session exporter.
func NewSessionExporter(session *AnnotatedSession, config *ExportConfig) *SessionExporter {
	if config == nil {
		config = DefaultExportConfig("")
	}
	if config.VideoWidth == 0 {
		config.VideoWidth = defaultVideoWidth
	}
	if config.VideoHeight == 0 {
		config.VideoHeight = defaultVideoHeight
	}
	if config.FontSize == 0 {
		config.FontSize = defaultFontSize
	}
	if config.FFmpegPath == "" {
		config.FFmpegPath = defaultFFmpeg
	}
	if config.AudioMix == "" {
		config.AudioMix = audioMixStereo
	}

	return &SessionExporter{
		session: session,
		config:  config,
	}
}

// Export exports the session to the configured format.
func (e *SessionExporter) Export(ctx context.Context) error {
	switch e.config.Format {
	case ExportFormatJSON:
		return e.exportJSON(ctx)
	case ExportFormatWAV, ExportFormatMP3:
		return e.exportAudio(ctx)
	case ExportFormatMP4, ExportFormatWebM:
		return e.exportVideo(ctx)
	default:
		return fmt.Errorf("unsupported format: %s", e.config.Format)
	}
}

// exportJSON exports the session as a JSON timeline.
func (e *SessionExporter) exportJSON(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	timeline := e.buildJSONTimeline()

	data, err := json.MarshalIndent(timeline, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal timeline: %w", err)
	}

	if err := os.WriteFile(e.config.OutputPath, data, filePermissions); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// JSONTimeline is the JSON export format.
type JSONTimeline struct {
	SessionID string             `json:"session_id"`
	Duration  float64            `json:"duration_seconds"`
	Metadata  SessionMetadata    `json:"metadata"`
	Events    []JSONTimelineItem `json:"events"`
	Tracks    []JSONTrack        `json:"tracks"`
}

// JSONTimelineItem is a single item in the JSON timeline.
type JSONTimelineItem struct {
	Time     float64                `json:"time_seconds"`
	Duration float64                `json:"duration_seconds,omitempty"`
	Type     string                 `json:"type"`
	Data     map[string]interface{} `json:"data"`
}

// JSONTrack is a media track in the JSON timeline.
type JSONTrack struct {
	Type     string        `json:"type"`
	Duration float64       `json:"duration_seconds"`
	Segments []JSONSegment `json:"segments"`
}

// JSONSegment is a media segment in the JSON timeline.
type JSONSegment struct {
	StartTime  float64 `json:"start_time_seconds"`
	Duration   float64 `json:"duration_seconds"`
	StorageRef string  `json:"storage_ref"`
	Size       int64   `json:"size_bytes"`
}

// buildJSONTimeline builds the JSON timeline structure.
//
//nolint:gocognit // Timeline building has inherent complexity
func (e *SessionExporter) buildJSONTimeline() *JSONTimeline {
	timeline := &JSONTimeline{
		SessionID: e.session.SessionID,
		Duration:  e.session.Metadata.Duration.Seconds(),
		Metadata:  e.session.Metadata,
	}

	// Add events
	for _, event := range e.session.Events {
		eventTime := event.Timestamp.Sub(e.session.Metadata.StartTime)
		if e.config.StartTime > 0 && eventTime < e.config.StartTime {
			continue
		}
		if e.config.EndTime > 0 && eventTime > e.config.EndTime {
			continue
		}

		item := JSONTimelineItem{
			Time: eventTime.Seconds(),
			Type: string(event.Type),
			Data: make(map[string]interface{}),
		}

		// Extract relevant data based on event type
		switch data := event.Data.(type) {
		case *MessageCreatedData:
			item.Data["role"] = data.Role
			item.Data["content"] = data.Content
		case *AudioTranscriptionData:
			item.Data["text"] = data.Text
			item.Data["language"] = data.Language
		case *ToolCallStartedData:
			item.Data["tool_name"] = data.ToolName
		case *ProviderCallCompletedData:
			item.Data["provider"] = data.Provider
			item.Data["model"] = data.Model
			item.Data["cost"] = data.Cost
		}

		timeline.Events = append(timeline.Events, item)
	}

	// Add annotations as events
	for _, annot := range e.session.Annotations {
		annotTime := e.getAnnotationTime(annot)
		if e.config.StartTime > 0 && annotTime < e.config.StartTime {
			continue
		}
		if e.config.EndTime > 0 && annotTime > e.config.EndTime {
			continue
		}

		item := JSONTimelineItem{
			Time: annotTime.Seconds(),
			Type: "annotation." + string(annot.Type),
			Data: map[string]interface{}{
				"key":   annot.Key,
				"value": annot.Value,
			},
		}

		if annot.Target.Type == annotations.TargetTimeRange {
			item.Duration = annot.Target.EndTime.Sub(annot.Target.StartTime).Seconds()
		}

		timeline.Events = append(timeline.Events, item)
	}

	// Add tracks
	for trackType, track := range e.session.Timeline.Tracks {
		jsonTrack := JSONTrack{
			Type:     string(trackType),
			Duration: track.TotalDuration.Seconds(),
		}

		for _, seg := range track.Segments {
			jsonTrack.Segments = append(jsonTrack.Segments, JSONSegment{
				StartTime:  seg.StartTime.Seconds(),
				Duration:   seg.Duration.Seconds(),
				StorageRef: seg.Payload.StorageRef,
				Size:       seg.Payload.Size,
			})
		}

		timeline.Tracks = append(timeline.Tracks, jsonTrack)
	}

	return timeline
}

// getAnnotationTime returns the activation time for an annotation.
func (e *SessionExporter) getAnnotationTime(annot *annotations.Annotation) time.Duration {
	//nolint:exhaustive // Only handling time-based target types, others default to 0
	switch annot.Target.Type {
	case annotations.TargetTimeRange:
		return annot.Target.StartTime.Sub(e.session.Metadata.StartTime)
	case annotations.TargetEvent:
		if annot.Target.EventSequence >= 0 && int(annot.Target.EventSequence) < len(e.session.Events) {
			event := e.session.Events[annot.Target.EventSequence]
			return event.Timestamp.Sub(e.session.Metadata.StartTime)
		}
	}
	return 0
}

// wantsInputAudio returns true if the audio mix mode includes input audio.
func (e *SessionExporter) wantsInputAudio() bool {
	return e.config.AudioMix == audioMixStereo ||
		e.config.AudioMix == audioMixInput ||
		e.config.AudioMix == audioMixMono
}

// wantsOutputAudio returns true if the audio mix mode includes output audio.
func (e *SessionExporter) wantsOutputAudio() bool {
	return e.config.AudioMix == audioMixStereo ||
		e.config.AudioMix == audioMixOutput ||
		e.config.AudioMix == audioMixMono
}

// exportAudio exports the session as an audio file.
func (e *SessionExporter) exportAudio(ctx context.Context) error {
	// Create temp directory for intermediate files
	tempDir, err := os.MkdirTemp("", "session-export-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Write audio segments to temp files
	var inputPaths []string
	var outputPaths []string

	if e.session.Timeline.HasTrack(TrackAudioInput) && e.wantsInputAudio() {
		path, err := e.writeTrackToFile(ctx, TrackAudioInput, tempDir, "input")
		if err != nil {
			return err
		}
		inputPaths = append(inputPaths, path)
	}

	if e.session.Timeline.HasTrack(TrackAudioOutput) && e.wantsOutputAudio() {
		path, err := e.writeTrackToFile(ctx, TrackAudioOutput, tempDir, "output")
		if err != nil {
			return err
		}
		outputPaths = append(outputPaths, path)
	}

	// Build ffmpeg command
	args := e.buildAudioFFmpegArgs(inputPaths, outputPaths, tempDir)
	if len(args) == 0 {
		return fmt.Errorf("no audio tracks available")
	}

	return e.runFFmpeg(ctx, args)
}

// writeTrackToFile writes a media track to a temporary file.
func (e *SessionExporter) writeTrackToFile(
	_ context.Context,
	trackType TrackType,
	tempDir, prefix string,
) (string, error) {
	reader, err := e.session.Timeline.NewTrackReader(trackType)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	path := filepath.Join(tempDir, prefix+".raw")
	f, err := os.Create(path) //nolint:gosec // path is constructed from trusted tempDir
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, reader)
	if err != nil {
		return "", fmt.Errorf("write audio data: %w", err)
	}

	return path, nil
}

// buildAudioFFmpegArgs builds ffmpeg arguments for audio export.
func (e *SessionExporter) buildAudioFFmpegArgs(inputPaths, outputPaths []string, _ string) []string {
	var args []string

	// Get audio format from timeline
	sampleRate, channels := e.getAudioFormat()

	// Add input files
	for _, path := range inputPaths {
		args = append(args, "-f", "s16le", "-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels), "-i", path)
	}
	for _, path := range outputPaths {
		args = append(args, "-f", "s16le", "-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels), "-i", path)
	}

	// Add filter for mixing
	totalInputs := len(inputPaths) + len(outputPaths)
	if totalInputs == 0 {
		return nil
	}

	if totalInputs == 2 && e.config.AudioMix == audioMixStereo {
		// Mix to stereo (left = input, right = output)
		args = append(args, ffmpegFilterComplex,
			"[0:a][1:a]amerge=inputs=2,pan=stereo|c0=c0|c1=c1[a]",
			"-map", "[a]")
	} else if totalInputs == 2 && e.config.AudioMix == audioMixMono {
		// Mix to mono
		args = append(args, ffmpegFilterComplex,
			"[0:a][1:a]amix=inputs=2:duration=longest[a]",
			"-map", "[a]")
	} else if totalInputs == 1 {
		args = append(args, "-map", "0:a")
	}

	// Add output format
	//nolint:exhaustive // Only handling audio formats here
	switch e.config.Format {
	case ExportFormatWAV:
		args = append(args, "-c:a", "pcm_s16le")
	case ExportFormatMP3:
		args = append(args, "-c:a", "libmp3lame", "-q:a", "2")
	}

	args = append(args, "-y", e.config.OutputPath)
	return args
}

// getAudioFormat returns the audio sample rate and channels from the timeline.
func (e *SessionExporter) getAudioFormat() (sampleRate, channels int) {
	sampleRate = 24000
	channels = 1
	if e.session.Timeline.HasTrack(TrackAudioInput) {
		if track := e.session.Timeline.GetTrack(TrackAudioInput); track != nil {
			if meta, ok := track.Format.(AudioMetadata); ok {
				sampleRate = meta.SampleRate
				channels = meta.Channels
			}
		}
	}
	return sampleRate, channels
}

// exportVideo exports the session as a video file with overlays.
func (e *SessionExporter) exportVideo(ctx context.Context) error {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "session-export-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Generate subtitle file for transcriptions/annotations
	subtitlePath := filepath.Join(tempDir, "subtitles.srt")
	if err := e.generateSubtitles(subtitlePath); err != nil {
		return err
	}

	// Write audio tracks
	var audioPaths []string
	if e.session.Timeline.HasTrack(TrackAudioInput) {
		path, err := e.writeTrackToFile(ctx, TrackAudioInput, tempDir, "input")
		if err != nil {
			return err
		}
		audioPaths = append(audioPaths, path)
	}
	if e.session.Timeline.HasTrack(TrackAudioOutput) {
		path, err := e.writeTrackToFile(ctx, TrackAudioOutput, tempDir, "output")
		if err != nil {
			return err
		}
		audioPaths = append(audioPaths, path)
	}

	// Build ffmpeg command
	args := e.buildVideoFFmpegArgs(audioPaths, subtitlePath)
	return e.runFFmpeg(ctx, args)
}

// generateSubtitles generates an SRT subtitle file from transcriptions and annotations.
//
//nolint:gocognit // Subtitle generation has inherent complexity
func (e *SessionExporter) generateSubtitles(path string) error {
	f, err := os.Create(path) //nolint:gosec // path is constructed from trusted tempDir
	if err != nil {
		return fmt.Errorf("create subtitle file: %w", err)
	}
	defer f.Close()

	index := 1

	// Add transcriptions
	if e.config.IncludeTranscriptions {
		for _, event := range e.session.Events {
			if event.Type != EventAudioTranscription {
				continue
			}
			data, ok := event.Data.(*AudioTranscriptionData)
			if !ok {
				continue
			}

			startTime := event.Timestamp.Sub(e.session.Metadata.StartTime)
			endTime := startTime + subtitleDuration

			_, _ = fmt.Fprintf(f, "%d\n", index)
			_, _ = fmt.Fprintf(f, "%s --> %s\n", formatSRTTime(startTime), formatSRTTime(endTime))
			_, _ = fmt.Fprintf(f, "%s\n\n", data.Text)
			index++
		}
	}

	// Add annotations
	if e.config.IncludeAnnotations {
		for _, annot := range e.session.Annotations {
			annotTime := e.getAnnotationTime(annot)
			var duration time.Duration
			if annot.Target.Type == annotations.TargetTimeRange {
				duration = annot.Target.EndTime.Sub(annot.Target.StartTime)
			} else {
				duration = subtitleDuration
			}

			text := e.formatAnnotation(annot)
			if text == "" {
				continue
			}

			_, _ = fmt.Fprintf(f, "%d\n", index)
			_, _ = fmt.Fprintf(f, "%s --> %s\n", formatSRTTime(annotTime), formatSRTTime(annotTime+duration))
			_, _ = fmt.Fprintf(f, "[%s] %s\n\n", annot.Key, text)
			index++
		}
	}

	return nil
}

// formatSRTTime formats a duration for SRT subtitles.
//
//nolint:mnd // Time conversion uses standard values (60s/min, 60min/hr, 1000ms/s)
func formatSRTTime(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	millis := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, millis)
}

// formatAnnotation formats an annotation for display.
func (e *SessionExporter) formatAnnotation(annot *annotations.Annotation) string {
	//nolint:exhaustive // Only displaying human-readable annotation types
	switch annot.Type {
	case annotations.TypeScore:
		if annot.Value.Score != nil {
			return fmt.Sprintf("%.2f", *annot.Value.Score)
		}
	case annotations.TypeLabel:
		return annot.Value.Label
	case annotations.TypeComment:
		return annot.Value.Text
	case annotations.TypeFlag:
		if annot.Value.Flag != nil && *annot.Value.Flag {
			return "Flagged"
		}
	case annotations.TypeAssertion:
		if annot.Value.Passed != nil {
			if *annot.Value.Passed {
				return "PASS"
			}
			return "FAIL: " + annot.Value.Message
		}
	}
	return ""
}

// buildVideoFFmpegArgs builds ffmpeg arguments for video export.
//
//nolint:gocognit // FFmpeg argument building has inherent complexity
func (e *SessionExporter) buildVideoFFmpegArgs(audioPaths []string, subtitlePath string) []string {
	var args []string

	// Get audio format
	sampleRate, channels := e.getAudioFormat()

	// Generate blank video with audio
	duration := e.session.Metadata.Duration
	if e.config.EndTime > 0 {
		duration = e.config.EndTime
	}
	if e.config.StartTime > 0 {
		duration -= e.config.StartTime
	}

	// Create color source for video (30 fps is standard)
	args = append(args, "-f", "lavfi", "-i",
		fmt.Sprintf("color=c=black:s=%dx%d:d=%f:r=30",
			e.config.VideoWidth, e.config.VideoHeight, duration.Seconds()))

	// Add audio inputs
	for _, path := range audioPaths {
		args = append(args, "-f", "s16le", "-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels), "-i", path)
	}

	// Build filter complex
	var filterParts []string

	// Add subtitle overlay if present
	if e.config.IncludeTranscriptions || e.config.IncludeAnnotations {
		subtitleFilter := fmt.Sprintf("subtitles=%s:force_style='FontSize=%d,PrimaryColour=&H00FFFFFF'",
			strings.ReplaceAll(subtitlePath, "'", "'\\''"), e.config.FontSize)
		filterParts = append(filterParts, fmt.Sprintf("[0:v]%s[v]", subtitleFilter))
	}

	// Add audio mixing if multiple tracks
	if len(audioPaths) == 2 { //nolint:mnd // 2 tracks = stereo mixing
		switch e.config.AudioMix {
		case audioMixStereo:
			filterParts = append(filterParts, "[1:a][2:a]amerge=inputs=2,pan=stereo|c0=c0|c1=c1[a]")
		case audioMixMono:
			filterParts = append(filterParts, "[1:a][2:a]amix=inputs=2:duration=longest[a]")
		}
	} else if len(audioPaths) == 1 {
		filterParts = append(filterParts, "[1:a]acopy[a]")
	}

	if len(filterParts) > 0 {
		args = append(args, ffmpegFilterComplex, strings.Join(filterParts, ";"))
		if e.config.IncludeTranscriptions || e.config.IncludeAnnotations {
			args = append(args, "-map", "[v]")
		} else {
			args = append(args, "-map", "0:v")
		}
		if len(audioPaths) > 0 {
			args = append(args, "-map", "[a]")
		}
	}

	// Output format
	//nolint:exhaustive // Only handling video formats here
	switch e.config.Format {
	case ExportFormatMP4:
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-c:a", "aac")
	case ExportFormatWebM:
		args = append(args, "-c:v", "libvpx-vp9", "-c:a", "libopus")
	}

	args = append(args, "-y", e.config.OutputPath)
	return args
}

// runFFmpeg executes ffmpeg with the given arguments.
func (e *SessionExporter) runFFmpeg(ctx context.Context, args []string) error {
	//nolint:gosec // FFmpegPath is intentionally configurable by the caller
	cmd := exec.CommandContext(ctx, e.config.FFmpegPath, args...)
	cmd.Stderr = os.Stderr // Show ffmpeg output for debugging

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	return nil
}

// ExportSession is a convenience function to export a session.
func ExportSession(
	ctx context.Context,
	session *AnnotatedSession,
	outputPath string,
	format ExportFormat,
) error {
	config := DefaultExportConfig(outputPath)
	config.Format = format
	exporter := NewSessionExporter(session, config)
	return exporter.Export(ctx)
}
