package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
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

func TestEnableMockProviderMode(t *testing.T) {
	// Create config with multiple providers
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
		LoadedProviders: map[string]*config.Provider{
			"mock1": {
				ID:               "mock1",
				Type:             "openai",
				Model:            "gpt-4",
				IncludeRawOutput: false,
			},
			"mock2": {
				ID:               "mock2",
				Type:             "anthropic",
				Model:            "claude-3-sonnet-20240229",
				IncludeRawOutput: true,
			},
		},
	}

	// Create provider registry manually
	providerRegistry := providers.NewRegistry()
	for _, provider := range cfg.LoadedProviders {
		// Create mock providers for the initial state (to avoid needing API keys)
		mockProvider := providers.NewMockProvider(provider.ID, provider.Model, provider.IncludeRawOutput)
		providerRegistry.Register(mockProvider)
	}

	// Create minimal engine using the direct constructor
	eng, err := NewEngine(cfg, providerRegistry, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Verify provider registry exists
	if eng.providerRegistry == nil {
		t.Fatal("Provider registry is nil")
	}

	// Verify provider configs are loaded
	if len(eng.providers) != 2 {
		t.Fatalf("Expected 2 provider configs, got %d", len(eng.providers))
	}

	// Enable mock provider mode (should replace providers in registry)
	err = eng.EnableMockProviderMode("")
	if err != nil {
		t.Fatalf("EnableMockProviderMode failed: %v", err)
	}

	// Verify providers are still accessible from registry
	mock1, ok := eng.providerRegistry.Get("mock1")
	if !ok {
		t.Fatal("Mock provider mock1 not found in registry after EnableMockProviderMode")
	}
	if mock1.ID() != "mock1" {
		t.Errorf("Expected mock provider ID mock1, got %s", mock1.ID())
	}

	mock2, ok := eng.providerRegistry.Get("mock2")
	if !ok {
		t.Fatal("Mock provider mock2 not found in registry after EnableMockProviderMode")
	}
	if mock2.ID() != "mock2" {
		t.Errorf("Expected mock provider ID mock2, got %s", mock2.ID())
	}

	// Verify the provider configs are still preserved
	if len(eng.providers) != 2 {
		t.Errorf("Expected 2 provider configs after mock mode, got %d", len(eng.providers))
	}
}

func TestEnableMockProviderMode_WithConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
		LoadedProviders: map[string]*config.Provider{
			"test-provider": {
				ID:    "test-provider",
				Type:  "openai",
				Model: "gpt-4",
			},
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)

	// Try to enable mock provider mode with a config file (not yet supported)
	err := eng.EnableMockProviderMode("/path/to/mock-config.yaml")
	if err == nil {
		t.Fatal("Expected error when using mock config file, got nil")
	}

	expectedErr := "mock configuration file support is not yet implemented (see #27)"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

