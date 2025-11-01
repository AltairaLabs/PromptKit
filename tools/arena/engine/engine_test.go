package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// testLogger is a helper struct for capturing and asserting on log output in tests.
// It automatically restores the original logger when the test completes.
//
// Example usage:
//
//	func TestSomething(t *testing.T) {
//		testLog := newTestLogger(t)
//
//		// Create engine (logger is not modified)
//		eng := NewEngine(cfg)
//
//		// Call function that logs
//		result := eng.SomeMethod()
//
//		// Assert on logs
//		testLog.assertContains("expected message")
//		testLog.assertNotContains("unexpected message")
//	}
type testLogger struct {
	buffer    *bytes.Buffer
	oldLogger *slog.Logger
	t         *testing.T
}

// newTestLogger creates a test logger that captures log output to a buffer
// and automatically restores the original logger when cleanup is called.
func newTestLogger(t *testing.T) *testLogger {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})

	tl := &testLogger{
		buffer:    &buf,
		oldLogger: logger.DefaultLogger,
		t:         t,
	}

	logger.DefaultLogger = slog.New(handler)

	// Register cleanup to restore original logger
	t.Cleanup(func() {
		logger.DefaultLogger = tl.oldLogger
	})

	return tl
}

// assertContains checks if the log output contains the expected string
func (tl *testLogger) assertContains(expected string) {
	tl.t.Helper()
	output := tl.buffer.String()
	if !strings.Contains(output, expected) {
		tl.t.Errorf("Expected log output to contain %q, got: %q", expected, output)
	}
}

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	if eng == nil {
		t.Fatal("newTestEngine returned nil")
	}

	// Verify engine is properly initialized
	if eng.stateStore == nil {
		t.Error("Engine state store is nil")
	}
}

func TestGenerateRunPlan_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)

	// Generate plan with no filters - should return empty combinations
	plan, err := eng.GenerateRunPlan(nil, nil, nil)
	if err != nil {
		t.Fatalf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Generated plan is nil")
	}

	// With no scenarios or providers loaded, combinations will be nil or empty
	// Both are valid since the triple nested loop won't execute
	if len(plan.Combinations) != 0 {
		t.Errorf("Expected 0 combinations, got %d", len(plan.Combinations))
	}
}

func TestExecuteRuns_EmptyPlan(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	ctx := context.Background()

	// Execute with empty plan
	plan := &RunPlan{
		Combinations: []RunCombination{},
	}

	runIDs, err := eng.ExecuteRuns(ctx, plan, 1)
	if err != nil {
		t.Fatalf("ExecuteRuns failed: %v", err)
	}

	if runIDs == nil {
		t.Fatal("RunIDs is nil")
	}

	if len(runIDs) != 0 {
		t.Errorf("Expected 0 runIDs, got %d", len(runIDs))
	}
}

func TestExecuteRuns_InvalidScenario(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	ctx := context.Background()

	// Execute with invalid scenario
	plan := &RunPlan{
		Combinations: []RunCombination{
			{
				Region:     "us",
				ScenarioID: "nonexistent",
				ProviderID: "test-provider",
			},
		},
	}

	results, err := eng.ExecuteRuns(ctx, plan, 1)
	if err != nil {
		t.Fatalf("ExecuteRuns failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Retrieve result from statestore
	arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		t.Fatal("StateStore is not ArenaStateStore")
	}

	result, err := arenaStore.GetRunResult(ctx, results[0])
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Result should have an error
	if result.Error == "" {
		t.Error("Expected error for nonexistent scenario, got none")
	}

	if result.Error != "scenario not found: nonexistent" {
		t.Errorf("Expected 'scenario not found' error, got: %s", result.Error)
	}
}
