//go:build arena_integration
// +build arena_integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExamplesIntegration_VariablesDemo runs the variables-demo example end-to-end
// using the engine+pipeline with MockProvider and verifies outputs and metadata.
func TestExamplesIntegration_VariablesDemo(t *testing.T) {
	// Use local schemas for deterministic parsing
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")

	// Locate example config
	repoRoot, err := os.Getwd()
	require.NoError(t, err)
	// Examples live at ../../../../examples relative to this package directory
	examplesDir := filepath.Join(repoRoot, "../../../../", "examples", "variables-demo")
	if _, statErr := os.Stat(examplesDir); statErr != nil {
		// Fallback if test is executed from repo root
		examplesDir = filepath.Join(repoRoot, "examples", "variables-demo")
	}

	// Config file within the example
	configFile := filepath.Join(examplesDir, "config.arena.yaml")
	if _, statErr := os.Stat(configFile); statErr != nil {
		t.Fatalf("example config not found at %s: %v", configFile, statErr)
	}

	// Prepare output directory
	outDir := t.TempDir()

	// Load config and parameters using direct config load
	params := &RunParameters{
		Regions:       []string{},
		Providers:     []string{"mock-provider"}, // Avoid selecting selfplay-mock as assistant
		Scenarios:     []string{"restaurant-default"},
		Concurrency:   1,
		OutDir:        outDir,
		CIMode:        true,  // Force headless execution
		Verbose:       false, // Keep logs quiet in tests
		OutputFormats: []string{"json", "junit", "html", "markdown"},
		MockProvider:  true,
		MockConfig:    filepath.Join(examplesDir, "mock-config.yaml"),
	}

	// Load configuration for defaults
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Apply default output file paths using config defaults
	setDefaultFilePaths(cfg, params)

	// Create engine and plan
	eng, plan, err := setupEngine(configFile, params)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	// Execute in CI/simple mode
	runIDs, err := executeWithMode(context.Background(), eng, plan, params)
	require.NoError(t, err)
	require.NotEmpty(t, runIDs)

	// Convert to engine results
	results, err := convertRunResults(context.Background(), eng, runIDs)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Save outputs
	err = processResults(results, params, configFile)
	require.NoError(t, err)

	// Verify output artifacts
	// - JSON index
	_, err = os.Stat(filepath.Join(outDir, "index.json"))
	assert.NoError(t, err, "index.json should exist")
	// - JUnit
	_, err = os.Stat(params.JUnitFile)
	assert.NoError(t, err, "JUnit file should exist")
	// - HTML
	_, err = os.Stat(params.HTMLFile)
	assert.NoError(t, err, "HTML report should exist")
	// - Markdown
	_, err = os.Stat(params.MarkdownFile)
	assert.NoError(t, err, "Markdown report should exist")

	// Validate conversation state metadata contains our counters
	arenaStore, ok := eng.GetStateStore().(*arenastore.ArenaStateStore)
	require.True(t, ok, "expected ArenaStateStore")
	for _, runID := range runIDs {
		state, loadErr := arenaStore.Load(context.Background(), runID)
		require.NoError(t, loadErr)
		if len(state.Messages) == 0 {
			continue
		}
		// Counters should be present and >= 1 after a successful turn
		if v, ok := state.Metadata["arena_user_completed_turns"].(int); ok {
			assert.GreaterOrEqual(t, v, 1)
		} else {
			t.Fatalf("missing arena_user_completed_turns in metadata for %s", runID)
		}
		if v, ok := state.Metadata["arena_assistant_completed_turns"].(int); ok {
			assert.GreaterOrEqual(t, v, 1)
		} else {
			t.Fatalf("missing arena_assistant_completed_turns in metadata for %s", runID)
		}
		// Mock turn number should be present for mock provider executions
		if mv, ok := state.Metadata["mock_turn_number"].(int); ok {
			assert.GreaterOrEqual(t, mv, 1)
		}
		break // One verified run is enough for integration coverage
	}
}

// Mock turn number should be present for mock provider executions
