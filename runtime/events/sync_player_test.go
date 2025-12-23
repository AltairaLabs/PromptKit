package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
)

func TestSyncPlayer_BasicPlayback(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi there"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(200 * time.Millisecond),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "How are you?"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	var deliveredEvents []*Event
	var mu sync.Mutex

	config := &SyncPlayerConfig{
		Speed:      10.0, // Fast playback for testing
		SkipTiming: true, // Skip timing delays
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			deliveredEvents = append(deliveredEvents, event)
			mu.Unlock()
			return true
		},
	}

	player := NewSyncPlayer(timeline, nil, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := player.Play(ctx); err != nil {
		t.Fatalf("Failed to start playback: %v", err)
	}

	player.Wait()

	mu.Lock()
	eventCount := len(deliveredEvents)
	mu.Unlock()

	if eventCount != 3 {
		t.Errorf("Expected 3 events, got %d", eventCount)
	}
}

func TestSyncPlayer_StateTransitions(t *testing.T) {
	sessionStart := time.Now()

	// Create events spanning several seconds to ensure playback doesn't complete immediately
	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(10 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "World"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	var states []PlayerState
	var mu sync.Mutex

	config := &SyncPlayerConfig{
		Speed: 1.0, // Normal speed
		OnStateChange: func(state PlayerState) {
			mu.Lock()
			states = append(states, state)
			mu.Unlock()
		},
	}

	player := NewSyncPlayer(timeline, nil, config)

	if player.State() != PlayerStateStopped {
		t.Error("Initial state should be stopped")
	}

	ctx := context.Background()
	player.Play(ctx)

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	if player.State() != PlayerStatePlaying {
		t.Errorf("State should be playing after Play(), got %v", player.State())
	}

	player.Pause()

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	if player.State() != PlayerStatePaused {
		t.Errorf("State should be paused after Pause(), got %v", player.State())
	}

	player.Stop()

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	if player.State() != PlayerStateStopped {
		t.Errorf("State should be stopped after Stop(), got %v", player.State())
	}
}

func TestSyncPlayer_Seek(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "First"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(1 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Second"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(2 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Third"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	player := NewSyncPlayer(timeline, nil, nil)

	// Seek to middle
	if err := player.Seek(1 * time.Second); err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	if player.Position() != 1*time.Second {
		t.Errorf("Expected position 1s, got %v", player.Position())
	}

	// Seek past end
	if err := player.Seek(10 * time.Second); err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	if player.Position() != player.Duration() {
		t.Errorf("Position should be clamped to duration")
	}

	// Seek before start
	if err := player.Seek(-1 * time.Second); err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	if player.Position() != 0 {
		t.Errorf("Position should be clamped to 0")
	}
}

func TestSyncPlayer_SpeedChange(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	config := &SyncPlayerConfig{Speed: 1.0}
	player := NewSyncPlayer(timeline, nil, config)

	player.SetSpeed(2.0)

	// Speed should be updated (we can't easily verify internal state, but ensure no crash)
	player.SetSpeed(0) // Should default to 1.0
	player.SetSpeed(-1) // Should default to 1.0
}

func TestSyncPlayer_WithAnnotations(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(1 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi"},
		},
	}

	annots := []*annotations.Annotation{
		{
			ID:        "annot-1",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "quality",
			Value:     annotations.NewScoreValue(0.9),
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 1,
			},
		},
		{
			ID:        "annot-2",
			Type:      annotations.TypeComment,
			SessionID: "test-session",
			Key:       "feedback",
			Value:     annotations.NewCommentValue("Good response"),
			Target: annotations.Target{
				Type:      annotations.TargetTimeRange,
				StartTime: sessionStart,
				EndTime:   sessionStart.Add(2 * time.Second),
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	var deliveredAnnotations []*annotations.Annotation
	var mu sync.Mutex

	config := &SyncPlayerConfig{
		Speed:      10.0,
		SkipTiming: true,
		OnAnnotation: func(annot *annotations.Annotation, position time.Duration) {
			mu.Lock()
			deliveredAnnotations = append(deliveredAnnotations, annot)
			mu.Unlock()
		},
	}

	player := NewSyncPlayer(timeline, annots, config)

	if player.AnnotationCount() != 2 {
		t.Errorf("Expected 2 annotations, got %d", player.AnnotationCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	player.Play(ctx)
	player.Wait()

	mu.Lock()
	annotCount := len(deliveredAnnotations)
	mu.Unlock()

	if annotCount != 2 {
		t.Errorf("Expected 2 annotations delivered, got %d", annotCount)
	}
}

func TestSyncPlayer_GetEventsInRange(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "First"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(500 * time.Millisecond),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Second"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(1 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Third"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	player := NewSyncPlayer(timeline, nil, nil)

	// Get events between 100ms and 800ms
	rangeEvents := player.GetEventsInRange(100*time.Millisecond, 800*time.Millisecond)

	if len(rangeEvents) != 1 {
		t.Errorf("Expected 1 event in range, got %d", len(rangeEvents))
	}
}

func TestSyncPlayer_Duration(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "First"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(5 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Last"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	player := NewSyncPlayer(timeline, nil, nil)

	if player.Duration() != 5*time.Second {
		t.Errorf("Expected duration 5s, got %v", player.Duration())
	}

	if player.EventCount() != 2 {
		t.Errorf("Expected 2 events, got %d", player.EventCount())
	}
}

func TestSyncPlayer_StopDuringPlayback(t *testing.T) {
	sessionStart := time.Now()

	// Create many events spanning a long time
	var events []*Event
	for i := 0; i < 100; i++ {
		events = append(events, &Event{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(time.Duration(i) * 100 * time.Millisecond),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Message"},
		})
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	var eventCount int
	var mu sync.Mutex

	config := &SyncPlayerConfig{
		Speed: 1.0, // Real-time
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			eventCount++
			mu.Unlock()
			return true
		},
	}

	player := NewSyncPlayer(timeline, nil, config)

	ctx := context.Background()
	player.Play(ctx)

	// Let it play for a bit
	time.Sleep(50 * time.Millisecond)

	// Stop playback
	player.Stop()

	mu.Lock()
	finalCount := eventCount
	mu.Unlock()

	// Should have delivered some events, but not all
	if finalCount >= 100 {
		t.Error("Stop should have interrupted playback")
	}

	// Player should be stopped
	if player.State() != PlayerStateStopped {
		t.Error("Player should be stopped")
	}

	// Position should be reset
	if player.Position() != 0 {
		t.Error("Position should be reset after stop")
	}
}

func TestSyncPlayer_Timeline(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	player := NewSyncPlayer(timeline, nil, nil)

	// Test Timeline() getter
	returnedTimeline := player.Timeline()
	if returnedTimeline != timeline {
		t.Error("Timeline() should return the same timeline")
	}
	if returnedTimeline.SessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got %s", returnedTimeline.SessionID)
	}
}

func TestSyncPlayer_Annotations(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
	}

	annots := []*annotations.Annotation{
		{
			ID:        "annot-1",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "quality",
			Value:     annotations.NewScoreValue(0.9),
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 0,
			},
		},
		{
			ID:        "annot-2",
			Type:      annotations.TypeComment,
			SessionID: "test-session",
			Key:       "feedback",
			Value:     annotations.NewCommentValue("Good"),
			Target: annotations.Target{
				Type:          annotations.TargetEvent,
				EventSequence: 0,
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	player := NewSyncPlayer(timeline, annots, nil)

	// Test Annotations() getter
	returnedAnnotations := player.Annotations()
	if len(returnedAnnotations) != 2 {
		t.Errorf("Expected 2 annotations, got %d", len(returnedAnnotations))
	}
}

func TestSyncPlayer_GetAnnotationsInRange(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "First"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(1 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "assistant", Content: "Second"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(2 * time.Second),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Third"},
		},
	}

	annots := []*annotations.Annotation{
		{
			ID:        "annot-1",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "early",
			Value:     annotations.NewScoreValue(0.5),
			Target: annotations.Target{
				Type:      annotations.TargetTimeRange,
				StartTime: sessionStart,
				EndTime:   sessionStart.Add(500 * time.Millisecond),
			},
		},
		{
			ID:        "annot-2",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "middle",
			Value:     annotations.NewScoreValue(0.7),
			Target: annotations.Target{
				Type:      annotations.TargetTimeRange,
				StartTime: sessionStart.Add(1 * time.Second),
				EndTime:   sessionStart.Add(1500 * time.Millisecond),
			},
		},
		{
			ID:        "annot-3",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "late",
			Value:     annotations.NewScoreValue(0.9),
			Target: annotations.Target{
				Type:      annotations.TargetTimeRange,
				StartTime: sessionStart.Add(2 * time.Second),
				EndTime:   sessionStart.Add(2500 * time.Millisecond),
			},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)
	player := NewSyncPlayer(timeline, annots, nil)

	// Get annotations in middle range (should get annot-2)
	rangeAnnots := player.GetAnnotationsInRange(800*time.Millisecond, 1600*time.Millisecond)
	if len(rangeAnnots) != 1 {
		t.Errorf("Expected 1 annotation in range 800ms-1600ms, got %d", len(rangeAnnots))
	}

	// Get annotations in full range (should get all 3)
	allAnnots := player.GetAnnotationsInRange(0, 3*time.Second)
	if len(allAnnots) != 3 {
		t.Errorf("Expected 3 annotations in full range, got %d", len(allAnnots))
	}

	// Get annotations in empty range
	noAnnots := player.GetAnnotationsInRange(3*time.Second, 4*time.Second)
	if len(noAnnots) != 0 {
		t.Errorf("Expected 0 annotations in empty range, got %d", len(noAnnots))
	}
}

func TestSyncPlayer_PlaybackCompletesNormally(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(50 * time.Millisecond),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi"},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	var completed bool
	var mu sync.Mutex

	config := &SyncPlayerConfig{
		Speed: 100.0, // Very fast for quick test
		OnComplete: func() {
			mu.Lock()
			completed = true
			mu.Unlock()
		},
	}

	player := NewSyncPlayer(timeline, nil, config)

	ctx := context.Background()
	if err := player.Play(ctx); err != nil {
		t.Fatalf("Failed to start playback: %v", err)
	}

	player.Wait()

	mu.Lock()
	wasCompleted := completed
	mu.Unlock()

	if !wasCompleted {
		t.Error("OnComplete callback should have been called")
	}

	if player.State() != PlayerStateStopped {
		t.Errorf("Player should be stopped after completion, got %v", player.State())
	}
}
