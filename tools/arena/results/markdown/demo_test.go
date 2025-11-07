package markdown_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/markdown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarkdownConfigurationFromYAML demonstrates the complete workflow
// of configuring markdown output via arena.yaml configuration file.
func TestMarkdownConfigurationFromYAML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an arena.yaml with modular output configuration
	arenaYAML := `apiVersion: "promptkit.altairalabs.ai/v1"
kind: "Arena"
metadata:
  name: "markdown-demo-config"
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1000
    # New modular output configuration
    output:
      dir: "test-output"
      formats: ["json", "markdown"]
      markdown:
        include_details: false      # Don't show detailed test information
        show_overview: true         # Show executive summary
        show_results_matrix: false  # Hide the results table
        show_failed_tests: true     # Show failed tests section
        show_cost_summary: false    # Hide cost analysis`

	configFile := filepath.Join(tmpDir, "arena.yaml")
	err := os.WriteFile(configFile, []byte(arenaYAML), 0644)
	require.NoError(t, err)

	// Load configuration (simulates what CLI does)
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Create markdown configuration from loaded defaults
	markdownConfig := markdown.CreateMarkdownConfigFromDefaults(&cfg.Defaults)

	// Verify the configuration matches our YAML settings
	assert.False(t, markdownConfig.IncludeDetails, "include_details should be false")
	assert.True(t, markdownConfig.ShowOverview, "show_overview should be true")
	assert.False(t, markdownConfig.ShowResultsMatrix, "show_results_matrix should be false")
	assert.True(t, markdownConfig.ShowFailedTests, "show_failed_tests should be true")
	assert.False(t, markdownConfig.ShowCostSummary, "show_cost_summary should be false")

	// Create markdown repository with the loaded configuration
	repo := markdown.NewMarkdownResultRepositoryWithConfig(tmpDir, markdownConfig)

	// Create some test results to see the configuration in action
	results := []engine.RunResult{
		{
			RunID:      "test-001",
			ScenarioID: "demo-scenario",
			ProviderID: "demo-provider",
			Region:     "us-east-1",
			Error:      "", // No error = success
		},
	}

	// Generate markdown (this would be called by the CLI)
	err = repo.SaveResults(results)
	require.NoError(t, err)

	// Read the generated markdown
	markdownFile := filepath.Join(tmpDir, "results.md")
	content, err := os.ReadFile(markdownFile)
	require.NoError(t, err)
	markdownContent := string(content)

	// Verify that the configuration was applied correctly
	assert.Contains(t, markdownContent, "# 🧪 PromptArena Test Results", "Should have main header")
	assert.Contains(t, markdownContent, "## 📊 Overview", "Should show overview section (configured as true)")
	assert.NotContains(t, markdownContent, "## 🔍 Test Results", "Should NOT show results matrix (configured as false)")
	// Note: Failed tests and cost sections only appear when there are actual failures/costs
}
