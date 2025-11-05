package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModularOutputConfiguration(t *testing.T) {
	tmpDir := t.TempDir()

	// Test new modular configuration structure
	arenaYAML := `apiVersion: "promptkit.ai/v1"
kind: "Arena"
metadata:
  name: "modular-output-test"
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1000
    output:
      dir: "custom-output"
      formats: ["json", "html", "markdown", "junit"]
      html:
        file: "custom-report.html"
      markdown:
        file: "custom-results.md"
        include_details: false
        show_overview: true
        show_results_matrix: false
        show_failed_tests: true
        show_cost_summary: false
      junit:
        file: "custom-results.xml"`

	configFile := filepath.Join(tmpDir, "arena.yaml")
	err := os.WriteFile(configFile, []byte(arenaYAML), 0644)
	require.NoError(t, err)

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Test that the output config is loaded correctly
	outputConfig := cfg.Defaults.GetOutputConfig()

	assert.Equal(t, "custom-output", outputConfig.Dir)
	assert.Equal(t, []string{"json", "html", "markdown", "junit"}, outputConfig.Formats)

	// Test HTML configuration
	htmlConfig := outputConfig.GetHTMLOutputConfig()
	assert.Equal(t, "custom-report.html", htmlConfig.File)

	// Test Markdown configuration
	markdownConfig := outputConfig.GetMarkdownOutputConfig()
	assert.Equal(t, "custom-results.md", markdownConfig.File)
	assert.False(t, markdownConfig.IncludeDetails)
	assert.True(t, markdownConfig.ShowOverview)
	assert.False(t, markdownConfig.ShowResultsMatrix)
	assert.True(t, markdownConfig.ShowFailedTests)
	assert.False(t, markdownConfig.ShowCostSummary)

	// Test JUnit configuration
	junitConfig := outputConfig.GetJUnitOutputConfig()
	assert.Equal(t, "custom-results.xml", junitConfig.File)
}

func TestBackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()

	// Test backward compatibility with old format
	arenaYAML := `apiVersion: "promptkit.ai/v1"
kind: "Arena"
metadata:
  name: "backward-compatibility-test"
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1000
    out_dir: "legacy-output"
    html_report: "legacy-report.html"
    output_formats: ["json", "html"]
    markdown_config:
      include_details: true
      show_overview: false
      show_results_matrix: true
      show_failed_tests: false
      show_cost_summary: true`

	configFile := filepath.Join(tmpDir, "arena.yaml")
	err := os.WriteFile(configFile, []byte(arenaYAML), 0644)
	require.NoError(t, err)

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Test that backward compatibility works
	outputConfig := cfg.Defaults.GetOutputConfig()

	assert.Equal(t, "legacy-output", outputConfig.Dir)
	assert.Equal(t, []string{"json", "html"}, outputConfig.Formats)

	// Test that HTML config is migrated
	htmlConfig := outputConfig.GetHTMLOutputConfig()
	assert.Equal(t, "legacy-report.html", htmlConfig.File)

	// Test that markdown config is migrated
	markdownConfig := outputConfig.GetMarkdownOutputConfig()
	assert.True(t, markdownConfig.IncludeDetails)
	assert.False(t, markdownConfig.ShowOverview)
	assert.True(t, markdownConfig.ShowResultsMatrix)
	assert.False(t, markdownConfig.ShowFailedTests)
	assert.True(t, markdownConfig.ShowCostSummary)
}

func TestGetMarkdownOutputConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		outputConfig *config.OutputConfig
		expected     *config.MarkdownOutputConfig
	}{
		{
			name: "nil markdown config returns default",
			outputConfig: &config.OutputConfig{
				Markdown: nil,
			},
			expected: &config.MarkdownOutputConfig{
				IncludeDetails:     true,
				ShowOverview:       true,
				ShowResultsMatrix:  true,
				ShowFailedTests:    true,
				ShowCostSummary:    true,
			},
		},
		{
			name: "empty markdown config returns as-is",
			outputConfig: &config.OutputConfig{
				Markdown: &config.MarkdownOutputConfig{},
			},
			expected: &config.MarkdownOutputConfig{
				IncludeDetails:     false, // Default zero values
				ShowOverview:       false,
				ShowResultsMatrix:  false,
				ShowFailedTests:    false,
				ShowCostSummary:    false,
			},
		},
		{
			name: "partial markdown config fills defaults",
			outputConfig: &config.OutputConfig{
				Markdown: &config.MarkdownOutputConfig{
					File:           "custom.md",
					IncludeDetails: false,
				},
			},
			expected: &config.MarkdownOutputConfig{
				File:               "custom.md",
				IncludeDetails:     false, // As set, others are zero values
				ShowOverview:       false,
				ShowResultsMatrix:  false,
				ShowFailedTests:    false,
				ShowCostSummary:    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.outputConfig.GetMarkdownOutputConfig()
			assert.Equal(t, tt.expected.File, result.File)
			assert.Equal(t, tt.expected.IncludeDetails, result.IncludeDetails)
			assert.Equal(t, tt.expected.ShowOverview, result.ShowOverview)
			assert.Equal(t, tt.expected.ShowResultsMatrix, result.ShowResultsMatrix)
			assert.Equal(t, tt.expected.ShowFailedTests, result.ShowFailedTests)
			assert.Equal(t, tt.expected.ShowCostSummary, result.ShowCostSummary)
		})
	}
}

func TestGetHTMLOutputConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		outputConfig *config.OutputConfig
		expected     *config.HTMLOutputConfig
	}{
		{
			name: "nil html config returns default",
			outputConfig: &config.OutputConfig{
				HTML: nil,
			},
			expected: &config.HTMLOutputConfig{
				File: "report.html", // Default value
			},
		},
		{
			name: "existing html config returned as-is",
			outputConfig: &config.OutputConfig{
				HTML: &config.HTMLOutputConfig{
					File: "custom.html",
				},
			},
			expected: &config.HTMLOutputConfig{
				File: "custom.html",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.outputConfig.GetHTMLOutputConfig()
			assert.Equal(t, tt.expected.File, result.File)
		})
	}
}

func TestGetJUnitOutputConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		outputConfig *config.OutputConfig
		expected     *config.JUnitOutputConfig
	}{
		{
			name: "nil junit config returns default",
			outputConfig: &config.OutputConfig{
				JUnit: nil,
			},
			expected: &config.JUnitOutputConfig{
				File: "results.xml", // Default value
			},
		},
		{
			name: "existing junit config returned as-is",
			outputConfig: &config.OutputConfig{
				JUnit: &config.JUnitOutputConfig{
					File: "custom.xml",
				},
			},
			expected: &config.JUnitOutputConfig{
				File: "custom.xml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.outputConfig.GetJUnitOutputConfig()
			assert.Equal(t, tt.expected.File, result.File)
		})
	}
}
