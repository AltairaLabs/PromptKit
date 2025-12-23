// Package main demonstrates session recording and replay.
//
// This example shows:
//   - Recording a conversation session to a file
//   - Exporting the session to a portable format
//   - Replaying the session deterministically
//
// Run with:
//
//	export GEMINI_API_KEY=your-key  # or OPENAI_API_KEY
//	go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/replay"
	"github.com/AltairaLabs/PromptKit/runtime/recording"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()

	// Create a temp directory for recordings
	recordingDir, err := os.MkdirTemp("", "promptkit-recording-*")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(recordingDir)

	fmt.Println("=== Session Recording Demo ===")
	fmt.Println()

	// Generate a session ID to track this conversation
	sessionID := uuid.New().String()

	// Step 1: Record a live session
	fmt.Println("Step 1: Recording a live conversation...")
	if err := recordSession(ctx, recordingDir, sessionID); err != nil {
		log.Fatalf("Failed to record session: %v", err)
	}
	fmt.Printf("   Session recorded: %s\n\n", sessionID)

	// Step 2: Export to portable format
	fmt.Println("Step 2: Exporting session to JSON...")
	exportPath := filepath.Join(recordingDir, "session.recording.json")
	if err := exportSession(ctx, recordingDir, sessionID, exportPath); err != nil {
		log.Fatalf("Failed to export session: %v", err)
	}
	fmt.Printf("   Exported to: %s\n\n", exportPath)

	// Step 3: Replay the session
	fmt.Println("Step 3: Replaying session with ReplayProvider...")
	if err := replaySession(ctx, exportPath); err != nil {
		log.Fatalf("Failed to replay session: %v", err)
	}

	// Step 4: Demonstrate playback with timing
	fmt.Println("\nStep 4: Playing back with SessionPlayer...")
	if err := playbackSession(ctx, recordingDir, sessionID); err != nil {
		log.Fatalf("Failed to playback session: %v", err)
	}

	fmt.Println("\n=== Demo Complete ===")
}

// recordSession records a live conversation
func recordSession(ctx context.Context, recordingDir, sessionID string) error {
	// Create an event store for recording
	store, err := events.NewFileEventStore(recordingDir)
	if err != nil {
		return fmt.Errorf("create event store: %w", err)
	}

	// Open conversation with recording enabled and explicit session ID
	conv, err := sdk.Open("./chat.pack.json", "chat",
		sdk.WithEventStore(store),
		sdk.WithConversationID(sessionID),
	)
	if err != nil {
		return fmt.Errorf("open pack: %w", err)
	}
	defer conv.Close()

	// Have a conversation
	messages := []string{
		"What is the capital of France?",
		"What's the population of that city?",
		"Thanks! One more question: what river runs through it?",
	}

	for _, msg := range messages {
		fmt.Printf("   User: %s\n", msg)
		resp, err := conv.Send(ctx, msg)
		if err != nil {
			return fmt.Errorf("send message: %w", err)
		}
		// Truncate response for display
		text := resp.Text()
		if len(text) > 100 {
			text = text[:100] + "..."
		}
		fmt.Printf("   Assistant: %s\n", text)
	}

	return nil
}

// exportSession exports a recorded session to a portable JSON file
func exportSession(ctx context.Context, recordingDir, sessionID, outputPath string) error {
	// Load events from store
	store, err := events.NewFileEventStore(recordingDir)
	if err != nil {
		return fmt.Errorf("open event store: %w", err)
	}

	// Query events for this session
	evts, err := store.Query(ctx, &events.EventFilter{SessionID: sessionID})
	if err != nil {
		return fmt.Errorf("query session events: %w", err)
	}

	// Build session recording
	now := time.Now()
	rec := &recording.SessionRecording{
		Metadata: recording.Metadata{
			SessionID:  sessionID,
			StartTime:  now,
			EndTime:    now,
			EventCount: len(evts),
			Version:    "1.0",
		},
	}

	// Convert events
	for i, e := range evts {
		data, _ := json.Marshal(e.Data)
		rec.Events = append(rec.Events, recording.RecordedEvent{
			Sequence:  int64(i + 1),
			Type:      e.Type,
			Timestamp: e.Timestamp,
			SessionID: sessionID,
			Data:      data,
		})
	}

	// Save to file
	if err := rec.SaveTo(outputPath, recording.FormatJSON); err != nil {
		return fmt.Errorf("save recording: %w", err)
	}

	fmt.Printf("   Saved %d events\n", len(rec.Events))
	return nil
}

// replaySession replays a recorded session using ReplayProvider
func replaySession(ctx context.Context, recordingPath string) error {
	// Load the recording
	rec, err := recording.Load(recordingPath)
	if err != nil {
		return fmt.Errorf("load recording: %w", err)
	}

	fmt.Printf("   Loaded recording with %d events\n", len(rec.Events))

	// Create replay provider
	provider, err := replay.NewProvider(rec, &replay.Config{
		Timing:    replay.TimingInstant, // Instant replay (no delays)
		MatchMode: replay.MatchByTurn,   // Sequential matching
	})
	if err != nil {
		return fmt.Errorf("create replay provider: %w", err)
	}

	fmt.Printf("   Created ReplayProvider with %d turns\n", provider.TurnCount())

	// Replay each turn
	for i := 0; i < provider.TurnCount(); i++ {
		resp, err := provider.Predict(ctx, providers.PredictionRequest{})
		if err != nil {
			return fmt.Errorf("predict turn %d: %w", i+1, err)
		}
		text := resp.Content
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		fmt.Printf("   Turn %d: %s\n", i+1, text)
	}

	return nil
}

// playbackSession demonstrates the SessionPlayer for timeline playback
func playbackSession(ctx context.Context, recordingDir, sessionID string) error {
	// Load events
	store, err := events.NewFileEventStore(recordingDir)
	if err != nil {
		return fmt.Errorf("open event store: %w", err)
	}

	// Create player with callback config
	var eventCount int
	player := events.NewSessionPlayer(store, sessionID, &events.PlayerConfig{
		Speed:      10.0, // 10x speed for demo
		SkipTiming: true, // Skip timing for quick demo
		OnEvent: func(event *events.Event, position time.Duration) bool {
			eventCount++
			fmt.Printf("   [%6.2fs] %s\n", position.Seconds(), event.Type)
			return true // Continue playback
		},
		OnComplete: func() {
			fmt.Printf("   Playback complete\n")
		},
	})

	// Load events into player
	if err := player.Load(ctx); err != nil {
		return fmt.Errorf("load events: %w", err)
	}

	// Play (blocking with SkipTiming)
	player.Play(ctx)
	player.Wait()

	fmt.Printf("   Played %d events\n", eventCount)
	return nil
}
