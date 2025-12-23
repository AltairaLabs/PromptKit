// Example of using ReplayPlayer for synchronized session replay with audio export.
//
// Usage:
//   go run replay_example.go [recording.jsonl]
//
// If no recording is provided, creates a demo recording with synthetic events.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/recording"
)

func main() {
	var recPath string
	var rec *recording.SessionRecording
	var err error

	if len(os.Args) > 1 {
		// Load provided recording
		recPath = os.Args[1]
		fmt.Printf("Loading recording: %s\n\n", recPath)
		rec, err = recording.Load(recPath)
		if err != nil {
			fmt.Printf("Failed to load recording: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Create a demo recording
		fmt.Println("Creating demo recording with audio...\n")
		rec, recPath = createDemoRecording()
	}

	// Show recording info
	fmt.Printf("Recording Info:\n")
	fmt.Printf("  Session ID:   %s\n", rec.Metadata.SessionID)
	fmt.Printf("  Duration:     %v\n", rec.Metadata.Duration)
	fmt.Printf("  Event Count:  %d\n", rec.Metadata.EventCount)
	fmt.Printf("  Provider:     %s\n", rec.Metadata.ProviderName)
	fmt.Printf("  Model:        %s\n", rec.Metadata.Model)
	fmt.Println()

	// Create replay player
	player, err := recording.NewReplayPlayer(rec)
	if err != nil {
		fmt.Printf("Failed to create player: %v\n", err)
		os.Exit(1)
	}

	// Add some demo annotations
	player.SetAnnotations(createDemoAnnotations(rec))

	// Show events by type
	fmt.Println("Events by Type:")
	showEventsByType(player)

	// Interactive playback simulation
	fmt.Println("\nSimulated Playback (100ms steps):")
	fmt.Println("─────────────────────────────────")
	simulatePlayback(player, 100*time.Millisecond)

	// Export audio if available
	exportAudio(player, recPath)

	fmt.Println("\n✓ Demo complete")
}

func createDemoRecording() (*recording.SessionRecording, string) {
	tmpDir, _ := os.MkdirTemp("", "replay-demo-")
	store, _ := events.NewFileEventStore(tmpDir)

	sessionID := "audio-demo-session"
	baseTime := time.Now()
	ctx := context.Background()

	// Generate synthetic audio (16kHz, 16-bit mono)
	inputAudio := make([]byte, 6400)  // 200ms
	outputAudio := make([]byte, 9600) // 200ms at 24kHz
	for i := range inputAudio {
		inputAudio[i] = byte((i * 3) % 256)
	}
	for i := range outputAudio {
		outputAudio[i] = byte((i * 5) % 256)
	}

	testEvents := []struct {
		offset time.Duration
		event  *events.Event
	}{
		{0, &events.Event{
			Type:      events.EventConversationStarted,
			SessionID: sessionID,
			Data:      &events.ConversationStartedData{SystemPrompt: "You are a helpful assistant."},
		}},
		{100 * time.Millisecond, &events.Event{
			Type:      events.EventMessageCreated,
			SessionID: sessionID,
			Data:      &events.MessageCreatedData{Role: "user", Content: "Hello, how are you?"},
		}},
		{200 * time.Millisecond, &events.Event{
			Type:      events.EventAudioInput,
			SessionID: sessionID,
			Data: &events.AudioInputData{
				Actor:      "user",
				ChunkIndex: 0,
				Payload:    events.BinaryPayload{InlineData: inputAudio, MIMEType: "audio/pcm", Size: int64(len(inputAudio))},
				Metadata:   events.AudioMetadata{SampleRate: 16000, Channels: 1, Encoding: "pcm_linear16", DurationMs: 200},
			},
		}},
		{500 * time.Millisecond, &events.Event{
			Type:      events.EventProviderCallStarted,
			SessionID: sessionID,
			Data:      &events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
		}},
		{800 * time.Millisecond, &events.Event{
			Type:      events.EventMessageCreated,
			SessionID: sessionID,
			Data:      &events.MessageCreatedData{Role: "assistant", Content: "I'm doing great! How can I help you today?"},
		}},
		{900 * time.Millisecond, &events.Event{
			Type:      events.EventAudioOutput,
			SessionID: sessionID,
			Data: &events.AudioOutputData{
				ChunkIndex:    0,
				Payload:       events.BinaryPayload{InlineData: outputAudio, MIMEType: "audio/pcm", Size: int64(len(outputAudio))},
				Metadata:      events.AudioMetadata{SampleRate: 24000, Channels: 1, Encoding: "pcm_linear16", DurationMs: 200},
				GeneratedFrom: "model",
			},
		}},
		{1200 * time.Millisecond, &events.Event{
			Type:      events.EventMessageCreated,
			SessionID: sessionID,
			Data:      &events.MessageCreatedData{Role: "user", Content: "Can you tell me a joke?"},
		}},
		{1500 * time.Millisecond, &events.Event{
			Type:      events.EventToolCallStarted,
			SessionID: sessionID,
			Data:      &events.ToolCallStartedData{ToolName: "joke_generator", CallID: "call-1"},
		}},
		{1700 * time.Millisecond, &events.Event{
			Type:      events.EventToolCallCompleted,
			SessionID: sessionID,
			Data:      &events.ToolCallCompletedData{ToolName: "joke_generator", CallID: "call-1", Status: "success"},
		}},
		{2 * time.Second, &events.Event{
			Type:      events.EventMessageCreated,
			SessionID: sessionID,
			Data:      &events.MessageCreatedData{Role: "assistant", Content: "Why don't scientists trust atoms? Because they make up everything!"},
		}},
	}

	for _, te := range testEvents {
		te.event.Timestamp = baseTime.Add(te.offset)
		store.Append(ctx, te.event)
	}
	store.Sync()
	store.Close()

	path := filepath.Join(tmpDir, sessionID+".jsonl")
	rec, _ := recording.Load(path)
	return rec, path
}

func createDemoAnnotations(rec *recording.SessionRecording) []*annotations.Annotation {
	return []*annotations.Annotation{
		{
			ID:        "quality-1",
			Type:      annotations.TypeScore,
			SessionID: rec.Metadata.SessionID,
			Target:    annotations.ForSession(),
			Key:       "overall_quality",
			Value:     annotations.NewScoreValue(0.92),
		},
		{
			ID:        "highlight-1",
			Type:      annotations.TypeComment,
			SessionID: rec.Metadata.SessionID,
			Target: annotations.InTimeRange(
				rec.Metadata.StartTime.Add(800*time.Millisecond),
				rec.Metadata.StartTime.Add(1200*time.Millisecond),
			),
			Key:   "highlight",
			Value: annotations.NewCommentValue("Good response time"),
		},
		{
			ID:        "joke-label",
			Type:      annotations.TypeLabel,
			SessionID: rec.Metadata.SessionID,
			Target: annotations.InTimeRange(
				rec.Metadata.StartTime.Add(1500*time.Millisecond),
				rec.Metadata.StartTime.Add(2*time.Second),
			),
			Key:   "content_type",
			Value: annotations.NewLabelValue("humor"),
		},
	}
}

func showEventsByType(player *recording.ReplayPlayer) {
	typeCounts := make(map[events.EventType]int)
	for _, e := range player.Recording().Events {
		typeCounts[e.Type]++
	}
	for t, count := range typeCounts {
		fmt.Printf("  %-30s %d\n", t, count)
	}
}

func simulatePlayback(player *recording.ReplayPlayer, step time.Duration) {
	duration := player.Duration()

	for pos := time.Duration(0); pos <= duration; pos += step {
		state := player.GetStateAt(pos)

		// Build status line
		line := fmt.Sprintf("[%s]", formatDuration(pos))

		// Show audio status
		if state.AudioInputActive {
			line += " 🎤"
		}
		if state.AudioOutputActive {
			line += " 🔊"
		}

		// Show current events
		for _, e := range state.CurrentEvents {
			switch e.Type {
			case events.EventMessageCreated:
				line += fmt.Sprintf(" 💬 %s", e.Type)
			case events.EventToolCallStarted, events.EventToolCallCompleted:
				line += fmt.Sprintf(" 🔧 %s", e.Type)
			case events.EventAudioInput, events.EventAudioOutput:
				// Already shown with icons
			default:
				line += fmt.Sprintf(" • %s", e.Type)
			}
		}

		// Show active annotations
		for _, ann := range state.ActiveAnnotations {
			if ann.Target.Type != annotations.TargetSession { // Skip session-level
				line += fmt.Sprintf(" 📝[%s]", ann.Key)
			}
		}

		// Only print lines with events or audio
		if len(state.CurrentEvents) > 0 || state.AudioInputActive || state.AudioOutputActive {
			fmt.Println(line)
		}
	}

	// Show final message summary
	fmt.Println()
	fmt.Println("Messages in conversation:")
	state := player.GetStateAt(duration)
	for _, msg := range state.Messages {
		content := msg.Content
		if len(content) > 50 {
			content = content[:50] + "..."
		}
		fmt.Printf("  [%s] %s: %s\n", formatDuration(msg.Offset), msg.Role, content)
	}
}

func exportAudio(player *recording.ReplayPlayer, recPath string) {
	timeline := player.Timeline()

	outDir := filepath.Dir(recPath)

	// Export input audio if available
	if timeline.HasTrack(events.TrackAudioInput) {
		inputPath := filepath.Join(outDir, "user_audio.wav")
		if err := timeline.ExportAudioToWAV(events.TrackAudioInput, inputPath); err != nil {
			fmt.Printf("\n⚠ Could not export input audio: %v\n", err)
		} else {
			fmt.Printf("\n✓ Exported user audio to: %s\n", inputPath)
		}
	}

	// Export output audio if available
	if timeline.HasTrack(events.TrackAudioOutput) {
		outputPath := filepath.Join(outDir, "assistant_audio.wav")
		if err := timeline.ExportAudioToWAV(events.TrackAudioOutput, outputPath); err != nil {
			fmt.Printf("⚠ Could not export output audio: %v\n", err)
		} else {
			fmt.Printf("✓ Exported assistant audio to: %s\n", outputPath)
		}
	}

	if !timeline.HasTrack(events.TrackAudioInput) && !timeline.HasTrack(events.TrackAudioOutput) {
		fmt.Println("\n(No audio tracks in this recording)")
	}
}

func formatDuration(d time.Duration) string {
	secs := int(d.Seconds())
	millis := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%d.%03ds", secs, millis)
}
