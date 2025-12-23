package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestEnableSessionRecording(t *testing.T) {
	// Create a temporary directory for recordings
	tempDir, err := os.MkdirTemp("", "session-recording-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a minimal engine for testing
	engine := &Engine{}

	// Enable session recording
	err = engine.EnableSessionRecording(tempDir)
	if err != nil {
		t.Fatalf("EnableSessionRecording failed: %v", err)
	}

	// Verify recording directory is set
	if engine.GetRecordingDir() != tempDir {
		t.Errorf("Expected recording dir %s, got %s", tempDir, engine.GetRecordingDir())
	}

	// Verify recording path generation
	testRunID := "test-run-123"
	expectedPath := filepath.Join(tempDir, testRunID+".jsonl")
	if engine.GetRecordingPath(testRunID) != expectedPath {
		t.Errorf("Expected recording path %s, got %s", expectedPath, engine.GetRecordingPath(testRunID))
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}

func TestEnableSessionRecordingWithEventBus(t *testing.T) {
	// Create a temporary directory for recordings
	tempDir, err := os.MkdirTemp("", "session-recording-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a minimal engine for testing
	engine := &Engine{}

	// Create event bus first
	eventBus := events.NewEventBus()
	engine.SetEventBus(eventBus)

	// Enable session recording after event bus
	err = engine.EnableSessionRecording(tempDir)
	if err != nil {
		t.Fatalf("EnableSessionRecording failed: %v", err)
	}

	// Verify event bus has the store attached
	if eventBus.Store() == nil {
		t.Error("Event bus should have event store attached")
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}

func TestEnableSessionRecordingBeforeEventBus(t *testing.T) {
	// Create a temporary directory for recordings
	tempDir, err := os.MkdirTemp("", "session-recording-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a minimal engine for testing
	engine := &Engine{}

	// Enable session recording first
	err = engine.EnableSessionRecording(tempDir)
	if err != nil {
		t.Fatalf("EnableSessionRecording failed: %v", err)
	}

	// Create event bus after
	eventBus := events.NewEventBus()
	engine.SetEventBus(eventBus)

	// Verify event bus has the store attached
	if eventBus.Store() == nil {
		t.Error("Event bus should have event store attached when set after recording enabled")
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}

func TestSessionRecordingWritesEvents(t *testing.T) {
	// Create a temporary directory for recordings
	tempDir, err := os.MkdirTemp("", "session-recording-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create engine with recording enabled
	engine := &Engine{}
	eventBus := events.NewEventBus()

	// Enable session recording
	err = engine.EnableSessionRecording(tempDir)
	if err != nil {
		t.Fatalf("EnableSessionRecording failed: %v", err)
	}
	engine.SetEventBus(eventBus)

	// Emit an event
	testSessionID := "test-session-456"
	emitter := events.NewEmitter(eventBus, "test-run", testSessionID, "test-conv")
	emitter.EmitCustom(
		events.EventType("test.event"),
		"TestMiddleware",
		"test_event",
		map[string]interface{}{"key": "value"},
		"Test event message",
	)

	// Give the event time to be written
	time.Sleep(100 * time.Millisecond)

	// Verify the recording file was created
	recordingPath := filepath.Join(tempDir, testSessionID+".jsonl")
	if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
		t.Errorf("Recording file should exist at %s", recordingPath)
	}

	// Read the file content to verify event was written
	content, err := os.ReadFile(recordingPath)
	if err != nil {
		t.Fatalf("Failed to read recording file: %v", err)
	}

	if len(content) == 0 {
		t.Error("Recording file should not be empty")
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}

func TestGetRecordingPathWhenDisabled(t *testing.T) {
	// Create a minimal engine without recording enabled
	engine := &Engine{}

	// Recording path should be empty when not enabled
	if engine.GetRecordingPath("test-run") != "" {
		t.Error("GetRecordingPath should return empty string when recording is not enabled")
	}

	// Recording dir should be empty
	if engine.GetRecordingDir() != "" {
		t.Error("GetRecordingDir should return empty string when recording is not enabled")
	}
}

func TestRecordingIntegrationWithEventStore(t *testing.T) {
	// Create a temporary directory for recordings
	tempDir, err := os.MkdirTemp("", "session-recording-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create engine with recording enabled
	engine := &Engine{}
	eventBus := events.NewEventBus()

	// Enable session recording
	err = engine.EnableSessionRecording(tempDir)
	if err != nil {
		t.Fatalf("EnableSessionRecording failed: %v", err)
	}
	engine.SetEventBus(eventBus)

	// Get the event store
	store := eventBus.Store()
	if store == nil {
		t.Fatal("Event store should be available")
	}

	// Emit several events with the same session ID
	testSessionID := "integration-test-session"
	emitter := events.NewEmitter(eventBus, "test-run", testSessionID, "test-conv")

	for i := 0; i < 5; i++ {
		emitter.EmitCustom(
			events.EventType("test.event"),
			"TestMiddleware",
			"test_event",
			map[string]interface{}{"index": i},
			"Test event message",
		)
	}

	// Give events time to be written
	time.Sleep(200 * time.Millisecond)

	// Query the events back from the store
	queriedEvents, err := store.Query(context.Background(), &events.EventFilter{
		SessionID: testSessionID,
	})
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}

	if len(queriedEvents) != 5 {
		t.Errorf("Expected 5 events, got %d", len(queriedEvents))
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}
