package events

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
)

func TestAnnotatedSession_Metadata(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioInputData{
				Metadata: AudioMetadata{DurationMs: 500},
				Payload:  BinaryPayload{InlineData: make([]byte, 100)},
			},
		},
		{
			Type:      EventAudioOutput,
			Timestamp: sessionStart.Add(200 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioOutputData{
				Metadata: AudioMetadata{DurationMs: 300},
				Payload:  BinaryPayload{InlineData: make([]byte, 100)},
			},
		},
		{
			Type:      EventToolCallStarted,
			Timestamp: sessionStart.Add(300 * time.Millisecond),
			SessionID: "test-session",
			Data:      &ToolCallStartedData{ToolName: "search"},
		},
		{
			Type:      EventProviderCallStarted,
			Timestamp: sessionStart.Add(400 * time.Millisecond),
			SessionID: "test-session",
			Data:      &ProviderCallStartedData{Provider: "openai"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(500 * time.Millisecond),
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi there"},
		},
	}

	annots := []*annotations.Annotation{
		{
			ID:        "annot-1",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "quality",
		},
		{
			ID:        "annot-2",
			Type:      annotations.TypeLabel,
			SessionID: "test-session",
			Key:       "category",
		},
		{
			ID:        "annot-3",
			Type:      annotations.TypeScore,
			SessionID: "test-session",
			Key:       "relevance",
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	session := &AnnotatedSession{
		SessionID:   "test-session",
		Events:      events,
		Annotations: annots,
		Timeline:    timeline,
	}

	loader := &AnnotatedSessionLoader{computeMeta: true}
	session.Metadata = loader.computeMetadata(session)

	// Check metadata
	if session.Metadata.Duration != 500*time.Millisecond {
		t.Errorf("Expected duration 500ms, got %v", session.Metadata.Duration)
	}

	if !session.Metadata.HasAudioInput {
		t.Error("Expected HasAudioInput to be true")
	}

	if !session.Metadata.HasAudioOutput {
		t.Error("Expected HasAudioOutput to be true")
	}

	if session.Metadata.TotalAudioInputDuration != 500*time.Millisecond {
		t.Errorf("Expected 500ms audio input, got %v", session.Metadata.TotalAudioInputDuration)
	}

	if session.Metadata.TotalAudioOutputDuration != 300*time.Millisecond {
		t.Errorf("Expected 300ms audio output, got %v", session.Metadata.TotalAudioOutputDuration)
	}

	if session.Metadata.ToolCalls != 1 {
		t.Errorf("Expected 1 tool call, got %d", session.Metadata.ToolCalls)
	}

	if session.Metadata.ProviderCalls != 1 {
		t.Errorf("Expected 1 provider call, got %d", session.Metadata.ProviderCalls)
	}

	if session.Metadata.ConversationTurns != 1 {
		t.Errorf("Expected 1 conversation turn, got %d", session.Metadata.ConversationTurns)
	}

	// Check annotation counts
	if session.Metadata.AnnotationCounts[annotations.TypeScore] != 2 {
		t.Errorf("Expected 2 score annotations, got %d", session.Metadata.AnnotationCounts[annotations.TypeScore])
	}

	if session.Metadata.AnnotationCounts[annotations.TypeLabel] != 1 {
		t.Errorf("Expected 1 label annotation, got %d", session.Metadata.AnnotationCounts[annotations.TypeLabel])
	}
}

func TestAnnotatedSession_GetEventsByType(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{Type: EventMessageCreated, Timestamp: sessionStart, SessionID: "test"},
		{Type: EventToolCallStarted, Timestamp: sessionStart.Add(100 * time.Millisecond), SessionID: "test"},
		{Type: EventMessageCreated, Timestamp: sessionStart.Add(200 * time.Millisecond), SessionID: "test"},
		{Type: EventToolCallCompleted, Timestamp: sessionStart.Add(300 * time.Millisecond), SessionID: "test"},
	}

	session := &AnnotatedSession{
		SessionID: "test",
		Events:    events,
	}

	messages := session.GetEventsByType(EventMessageCreated)
	if len(messages) != 2 {
		t.Errorf("Expected 2 message events, got %d", len(messages))
	}

	toolStarts := session.GetEventsByType(EventToolCallStarted)
	if len(toolStarts) != 1 {
		t.Errorf("Expected 1 tool start event, got %d", len(toolStarts))
	}
}

func TestAnnotatedSession_GetAnnotationsByType(t *testing.T) {
	annots := []*annotations.Annotation{
		{ID: "1", Type: annotations.TypeScore},
		{ID: "2", Type: annotations.TypeLabel},
		{ID: "3", Type: annotations.TypeScore},
		{ID: "4", Type: annotations.TypeComment},
	}

	session := &AnnotatedSession{
		SessionID:   "test",
		Annotations: annots,
	}

	scores := session.GetAnnotationsByType(annotations.TypeScore)
	if len(scores) != 2 {
		t.Errorf("Expected 2 score annotations, got %d", len(scores))
	}

	comments := session.GetAnnotationsByType(annotations.TypeComment)
	if len(comments) != 1 {
		t.Errorf("Expected 1 comment annotation, got %d", len(comments))
	}
}

func TestAnnotatedSession_GetAnnotationsForEvent(t *testing.T) {
	annots := []*annotations.Annotation{
		{
			ID:     "1",
			Target: annotations.Target{Type: annotations.TargetEvent, EventSequence: 0},
		},
		{
			ID:     "2",
			Target: annotations.Target{Type: annotations.TargetEvent, EventSequence: 1},
		},
		{
			ID:     "3",
			Target: annotations.Target{Type: annotations.TargetEvent, EventSequence: 0},
		},
		{
			ID:     "4",
			Target: annotations.Target{Type: annotations.TargetSession},
		},
	}

	session := &AnnotatedSession{
		SessionID:   "test",
		Annotations: annots,
	}

	event0Annots := session.GetAnnotationsForEvent(0)
	if len(event0Annots) != 2 {
		t.Errorf("Expected 2 annotations for event 0, got %d", len(event0Annots))
	}

	event1Annots := session.GetAnnotationsForEvent(1)
	if len(event1Annots) != 1 {
		t.Errorf("Expected 1 annotation for event 1, got %d", len(event1Annots))
	}

	event2Annots := session.GetAnnotationsForEvent(2)
	if len(event2Annots) != 0 {
		t.Errorf("Expected 0 annotations for event 2, got %d", len(event2Annots))
	}
}

func TestAnnotatedSession_GetConversationMessages(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventToolCallStarted,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test",
		},
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart.Add(200 * time.Millisecond),
			SessionID: "test",
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi"},
		},
	}

	session := &AnnotatedSession{
		SessionID: "test",
		Events:    events,
	}

	messages := session.GetConversationMessages()
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}
}

func TestAnnotatedSession_Summary(t *testing.T) {
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

	timeline := NewMediaTimeline("test-session", events, nil)

	session := &AnnotatedSession{
		SessionID: "test-session",
		Events:    events,
		Timeline:  timeline,
		Metadata: SessionMetadata{
			Duration:          1 * time.Second,
			ConversationTurns: 1,
			HasAudioInput:     false,
			HasAudioOutput:    false,
		},
	}

	summary := session.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}

	if len(summary) < 20 {
		t.Error("Summary should contain meaningful content")
	}
}

func TestAnnotatedSession_BuildTimelineView(t *testing.T) {
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioInputData{
				Metadata: AudioMetadata{DurationMs: 200},
				Payload:  BinaryPayload{InlineData: make([]byte, 100)},
			},
		},
	}

	annots := []*annotations.Annotation{
		{
			ID:     "annot-1",
			Type:   annotations.TypeComment,
			Target: annotations.Target{Type: annotations.TargetEvent, EventSequence: 0},
		},
	}

	timeline := NewMediaTimeline("test-session", events, nil)

	session := &AnnotatedSession{
		SessionID:   "test-session",
		Events:      events,
		Annotations: annots,
		Timeline:    timeline,
		Metadata: SessionMetadata{
			StartTime: sessionStart,
		},
	}

	view := session.BuildTimelineView()

	if len(view.Items) == 0 {
		t.Error("Timeline view should have items")
	}

	// Check that items are sorted by time
	for i := 1; i < len(view.Items); i++ {
		if view.Items[i].Time < view.Items[i-1].Time {
			t.Error("Timeline items should be sorted by time")
		}
	}

	// Check item types
	hasEvent := false
	hasAnnotation := false
	hasMedia := false

	for _, item := range view.Items {
		switch item.Type {
		case TimelineItemEvent:
			hasEvent = true
		case TimelineItemAnnotation:
			hasAnnotation = true
		case TimelineItemMedia:
			hasMedia = true
		}
	}

	if !hasEvent {
		t.Error("Timeline should have event items")
	}
	if !hasAnnotation {
		t.Error("Timeline should have annotation items")
	}
	if !hasMedia {
		t.Error("Timeline should have media items")
	}
}

func TestAnnotatedSessionLoader_Load(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "annotated-session-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create event store
	eventStore, err := NewFileEventStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create event store: %v", err)
	}
	defer eventStore.Close()

	// Add events
	ctx := context.Background()
	sessionStart := time.Now()

	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: sessionStart,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventAudioInput,
			Timestamp: sessionStart.Add(100 * time.Millisecond),
			SessionID: "test-session",
			Data: &AudioInputData{
				Metadata: AudioMetadata{DurationMs: 100},
				Payload:  BinaryPayload{InlineData: make([]byte, 100)},
			},
		},
	}

	for _, event := range events {
		if err := eventStore.Append(ctx, event); err != nil {
			t.Fatalf("Failed to append event: %v", err)
		}
	}

	// Create loader (without annotation store for this test)
	loader := NewAnnotatedSessionLoader(eventStore, nil, nil)

	// Load session
	session, err := loader.Load(ctx, "test-session")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	if session.SessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got %s", session.SessionID)
	}

	if len(session.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(session.Events))
	}

	if session.Timeline == nil {
		t.Error("Timeline should not be nil")
	}

	if !session.Metadata.HasAudioInput {
		t.Error("Metadata should indicate audio input")
	}
}

func TestAnnotatedSession_NewSyncPlayer(t *testing.T) {
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

	session := &AnnotatedSession{
		SessionID: "test-session",
		Events:    events,
		Timeline:  timeline,
	}

	player := session.NewSyncPlayer(nil)

	if player == nil {
		t.Fatal("Player should not be nil")
	}

	if player.EventCount() != 1 {
		t.Errorf("Expected 1 event, got %d", player.EventCount())
	}
}
