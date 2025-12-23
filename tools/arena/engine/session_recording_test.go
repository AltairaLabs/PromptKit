package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
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

func TestConfigureSessionRecordingFromConfig_Enabled(t *testing.T) {
	// Create a temporary directory for output
	tempDir, err := os.MkdirTemp("", "session-recording-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create config with recording enabled
	cfg := &config.Config{
		Defaults: config.Defaults{
			Output: config.OutputConfig{
				Dir: tempDir,
				Recording: &config.RecordingConfig{
					Enabled: true,
					Dir:     "recordings",
				},
			},
		},
	}

	// Create engine with config
	engine := &Engine{config: cfg}

	// Configure session recording from config
	err = engine.ConfigureSessionRecordingFromConfig()
	if err != nil {
		t.Fatalf("ConfigureSessionRecordingFromConfig failed: %v", err)
	}

	// Verify recording directory is set
	expectedDir := filepath.Join(tempDir, "recordings")
	if engine.GetRecordingDir() != expectedDir {
		t.Errorf("Expected recording dir %s, got %s", expectedDir, engine.GetRecordingDir())
	}

	// Verify recording path generation works
	testRunID := "test-run-123"
	expectedPath := filepath.Join(expectedDir, testRunID+".jsonl")
	if engine.GetRecordingPath(testRunID) != expectedPath {
		t.Errorf("Expected recording path %s, got %s", expectedPath, engine.GetRecordingPath(testRunID))
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}

func TestConfigureSessionRecordingFromConfig_Disabled(t *testing.T) {
	// Create config with recording explicitly disabled
	cfg := &config.Config{
		Defaults: config.Defaults{
			Output: config.OutputConfig{
				Dir: "/tmp/test-out",
				Recording: &config.RecordingConfig{
					Enabled: false,
				},
			},
		},
	}

	// Create engine with config
	engine := &Engine{config: cfg}

	// Configure session recording from config
	err := engine.ConfigureSessionRecordingFromConfig()
	if err != nil {
		t.Fatalf("ConfigureSessionRecordingFromConfig failed: %v", err)
	}

	// Verify recording is not enabled
	if engine.GetRecordingDir() != "" {
		t.Errorf("Expected empty recording dir, got %s", engine.GetRecordingDir())
	}
}

func TestConfigureSessionRecordingFromConfig_NilRecording(t *testing.T) {
	// Create config without recording config
	cfg := &config.Config{
		Defaults: config.Defaults{
			Output: config.OutputConfig{
				Dir: "/tmp/test-out",
				// Recording is nil
			},
		},
	}

	// Create engine with config
	engine := &Engine{config: cfg}

	// Configure session recording from config
	err := engine.ConfigureSessionRecordingFromConfig()
	if err != nil {
		t.Fatalf("ConfigureSessionRecordingFromConfig failed: %v", err)
	}

	// Verify recording is not enabled
	if engine.GetRecordingDir() != "" {
		t.Errorf("Expected empty recording dir, got %s", engine.GetRecordingDir())
	}
}

func TestConfigureSessionRecordingFromConfig_DefaultDirs(t *testing.T) {
	// Create config with recording enabled but no dirs specified
	cfg := &config.Config{
		Defaults: config.Defaults{
			Output: config.OutputConfig{
				// Dir is empty - should default to "out"
				Recording: &config.RecordingConfig{
					Enabled: true,
					// Dir is empty - should default to "recordings"
				},
			},
		},
	}

	// Create engine with config
	engine := &Engine{config: cfg}

	// Configure session recording from config
	err := engine.ConfigureSessionRecordingFromConfig()
	if err != nil {
		t.Fatalf("ConfigureSessionRecordingFromConfig failed: %v", err)
	}

	// Verify default recording directory is set
	expectedDir := filepath.Join("out", "recordings")
	if engine.GetRecordingDir() != expectedDir {
		t.Errorf("Expected default recording dir %s, got %s", expectedDir, engine.GetRecordingDir())
	}

	// Clean up
	if err := engine.Close(); err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}
}

func TestGetConfig(t *testing.T) {
	// Create config
	cfg := &config.Config{
		Defaults: config.Defaults{
			Output: config.OutputConfig{
				Dir: "test-output",
			},
		},
	}

	// Create engine with config
	engine := &Engine{config: cfg}

	// Verify GetConfig returns the config
	if engine.GetConfig() != cfg {
		t.Error("GetConfig should return the engine's config")
	}
}
