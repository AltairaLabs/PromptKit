// Demo of session recording and synchronized playback.
//
// This demonstrates:
// 1. Recording events to a session
// 2. Loading the session with annotations
// 3. Playing it back with synchronized timing
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func main() {
	// Create temp directory for the demo
	tempDir, err := os.MkdirTemp("", "session-demo-")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	fmt.Println("=== Session Recording & Playback Demo ===\n")

	// Step 1: Create event store and record a session
	fmt.Println("1. Recording session events...")
	store, err := events.NewFileEventStore(tempDir)
	if err != nil {
		fmt.Printf("Failed to create store: %v\n", err)
		os.Exit(1)
	}

	sessionID := "demo-session"
	sessionStart := time.Now()
	ctx := context.Background()

	// Simulate a conversation with timing
	recordedEvents := []struct {
		offset  time.Duration
		evType  events.EventType
		data    events.EventData
		display string
	}{
		{
			offset:  0,
			evType:  events.EventMessageCreated,
			data:    &events.MessageCreatedData{Role: "user", Content: "Hello! What can you help me with?"},
			display: "[User] Hello! What can you help me with?",
		},
		{
			offset:  500 * time.Millisecond,
			evType:  events.EventProviderCallStarted,
			data:    &events.ProviderCallStartedData{Provider: "demo", Model: "demo-model"},
			display: "[System] Provider call started...",
		},
		{
			offset:  1 * time.Second,
			evType:  events.EventMessageCreated,
			data:    &events.MessageCreatedData{Role: "assistant", Content: "I'm an AI assistant. I can help you with questions, writing, coding, and more!"},
			display: "[Assistant] I'm an AI assistant. I can help you with questions, writing, coding, and more!",
		},
		{
			offset:  2 * time.Second,
			evType:  events.EventMessageCreated,
			data:    &events.MessageCreatedData{Role: "user", Content: "Can you explain what session replay is?"},
			display: "[User] Can you explain what session replay is?",
		},
		{
			offset:  2500 * time.Millisecond,
			evType:  events.EventToolCallStarted,
			data:    &events.ToolCallStartedData{ToolName: "search", CallID: "call-1"},
			display: "[System] Tool call: search",
		},
		{
			offset:  3 * time.Second,
			evType:  events.EventToolCallCompleted,
			data:    &events.ToolCallCompletedData{ToolName: "search", CallID: "call-1", Status: "success"},
			display: "[System] Tool call completed",
		},
		{
			offset:  3500 * time.Millisecond,
			evType:  events.EventMessageCreated,
			data:    &events.MessageCreatedData{Role: "assistant", Content: "Session replay is a feature that records all events during a conversation and allows you to play them back later with synchronized timing. It's useful for debugging, testing, and analysis."},
			display: "[Assistant] Session replay is a feature that records all events...",
		},
	}

	for _, ev := range recordedEvents {
		event := &events.Event{
			Type:      ev.evType,
			Timestamp: sessionStart.Add(ev.offset),
			SessionID: sessionID,
			Data:      ev.data,
		}
		if err := store.Append(ctx, event); err != nil {
			fmt.Printf("Failed to append event: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Recorded: %s (at %v)\n", ev.display[:min(50, len(ev.display))], ev.offset)
	}
	store.Close()

	fmt.Printf("\n  Session saved to: %s\n\n", filepath.Join(tempDir, sessionID+".jsonl"))

	// Step 2: Create some annotations
	fmt.Println("2. Creating annotations...")
	annotStore, err := annotations.NewFileStore(tempDir)
	if err != nil {
		fmt.Printf("Failed to create annotation store: %v\n", err)
		os.Exit(1)
	}

	annots := []*annotations.Annotation{
		{
			ID:        "quality-1",
			Type:      annotations.TypeScore,
			SessionID: sessionID,
			Key:       "response_quality",
			Value:     annotations.NewScoreValue(0.95),
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 2, // First assistant response
			},
		},
		{
			ID:        "label-1",
			Type:      annotations.TypeLabel,
			SessionID: sessionID,
			Key:       "topic",
			Value:     annotations.NewLabelValue("introduction"),
			Target: annotations.Target{
				Type: annotations.TargetSession,
			},
		},
		{
			ID:        "comment-1",
			Type:      annotations.TypeComment,
			SessionID: sessionID,
			Key:       "reviewer_note",
			Value:     annotations.NewCommentValue("Good explanation of session replay"),
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 6, // Final response
			},
		},
	}

	for _, annot := range annots {
		if err := annotStore.Add(ctx, annot); err != nil {
			fmt.Printf("Failed to add annotation: %v\n", err)
		}
		fmt.Printf("  Added: %s annotation '%s'\n", annot.Type, annot.Key)
	}
	annotStore.Close()

	// Step 3: Load the session
	fmt.Println("\n3. Loading annotated session...")
	store2, _ := events.NewFileEventStore(tempDir)
	annotStore2, _ := annotations.NewFileStore(tempDir)
	defer store2.Close()
	defer annotStore2.Close()

	loader := events.NewAnnotatedSessionLoader(store2, nil, annotStore2)
	session, err := loader.Load(ctx, sessionID)
	if err != nil {
		fmt.Printf("Failed to load session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  Session ID: %s\n", session.SessionID)
	fmt.Printf("  Duration: %v\n", session.Metadata.Duration)
	fmt.Printf("  Events: %d\n", len(session.Events))
	fmt.Printf("  Annotations: %d\n", len(session.Annotations))
	fmt.Printf("  Conversation turns: %d\n", session.Metadata.ConversationTurns)

	// Step 4: Play back with synchronized timing
	fmt.Println("\n4. Playing back session (2x speed)...\n")
	fmt.Println("--- Playback Start ---")

	playbackStart := time.Now()
	eventCount := 0
	annotCount := 0

	config := &events.SyncPlayerConfig{
		Speed: 2.0, // 2x playback speed
		OnEvent: func(event *events.Event, position time.Duration) bool {
			eventCount++
			prefix := "  "
			switch data := event.Data.(type) {
			case *events.MessageCreatedData:
				role := data.Role
				content := data.Content
				if len(content) > 60 {
					content = content[:60] + "..."
				}
				fmt.Printf("%s[%v] [%s] %s\n", prefix, position.Round(time.Millisecond), role, content)
			case *events.ToolCallStartedData:
				fmt.Printf("%s[%v] [tool] %s called\n", prefix, position.Round(time.Millisecond), data.ToolName)
			case *events.ProviderCallStartedData:
				fmt.Printf("%s[%v] [provider] %s started\n", prefix, position.Round(time.Millisecond), data.Provider)
			}
			return true
		},
		OnAnnotation: func(annot *annotations.Annotation, position time.Duration) {
			annotCount++
			fmt.Printf("  [%v] [annotation] %s: %s\n", position.Round(time.Millisecond), annot.Key, formatAnnotValue(annot))
		},
		OnComplete: func() {
			fmt.Println("--- Playback End ---")
		},
	}

	player := session.NewSyncPlayer(config)
	if err := player.Play(ctx); err != nil {
		fmt.Printf("Failed to start playback: %v\n", err)
		os.Exit(1)
	}
	player.Wait()

	realDuration := time.Since(playbackStart)
	fmt.Printf("\n  Played %d events and %d annotations\n", eventCount, annotCount)
	fmt.Printf("  Session duration: %v\n", session.Metadata.Duration)
	fmt.Printf("  Playback time: %v (at 2x speed)\n", realDuration.Round(time.Millisecond))

	// Step 5: Show summary
	fmt.Println("\n5. Session Summary:")
	fmt.Println(session.Summary())

	fmt.Println("\n=== Demo Complete ===")
}

func formatAnnotValue(annot *annotations.Annotation) string {
	switch annot.Type {
	case annotations.TypeScore:
		if annot.Value.Score != nil {
			return fmt.Sprintf("%.2f", *annot.Value.Score)
		}
	case annotations.TypeLabel:
		return annot.Value.Label
	case annotations.TypeComment:
		if len(annot.Value.Text) > 40 {
			return annot.Value.Text[:40] + "..."
		}
		return annot.Value.Text
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
