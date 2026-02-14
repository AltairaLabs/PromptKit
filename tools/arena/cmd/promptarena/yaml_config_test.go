package main

import (
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestYAMLConfigurationSupport(t *testing.T) {
	tests := []struct {
		name           string
		configFormats  []string
		flagFormats    []string
		flagChanged    bool
		expectedResult []string
	}{
		{
			name:           "no config, no flag - defaults to json",
			configFormats:  []string{},
			flagFormats:    []string{},
			flagChanged:    false,
			expectedResult: []string{"json"},
		},
		{
			name:           "config has formats, no flag - uses config",
			configFormats:  []string{"json", "html"},
			flagFormats:    []string{},
			flagChanged:    false,
			expectedResult: []string{"json", "html"},
		},
		{
			name:           "config has formats, flag set - uses flag",
			configFormats:  []string{"json", "html"},
			flagFormats:    []string{"junit"},
			flagChanged:    true,
			expectedResult: []string{"junit"},
		},
		{
			name:           "no config, flag set - uses flag",
			configFormats:  []string{},
			flagFormats:    []string{"html", "junit"},
			flagChanged:    true,
			expectedResult: []string{"html", "junit"},
		},
		{
			name:           "config has all formats - uses config",
			configFormats:  []string{"json", "junit", "html"},
			flagFormats:    []string{},
			flagChanged:    false,
			expectedResult: []string{"json", "junit", "html"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock config
			cfg := &config.Config{
				Defaults: config.Defaults{
					OutputFormats: tt.configFormats,
				},
			}

			// Create a mock command with flags
			cmd := &cobra.Command{}
			cmd.Flags().StringSlice("format", tt.flagFormats, "Output formats")
			if tt.flagChanged {
				// Simulate that the flag was changed by the user
				cmd.Flags().Set("format", formatSliceToString(tt.flagFormats))
			}

			// Test the extraction logic
			params := &RunParameters{}
			var err error

			// Extract output format flags - use config default if not specified via flag
			if params.OutputFormats, err = cmd.Flags().GetStringSlice("format"); err != nil {
				t.Fatalf("failed to get format flag: %v", err)
			}

			// If format flag wasn't changed, use config defaults, otherwise fallback to json
			if !cmd.Flags().Changed("format") {
				if len(cfg.Defaults.OutputFormats) > 0 {
					params.OutputFormats = cfg.Defaults.OutputFormats
				} else {
					params.OutputFormats = []string{"json"} // Default fallback
				}
			}

			// Verify the result
			assert.Equal(t, tt.expectedResult, params.OutputFormats)
		})
	}
}

func TestExtractRunParameters_OutputFormats(t *testing.T) {
	tests := []struct {
		name           string
		configFormats  []string
		flagValue      []string
		setFlag        bool
		expectedResult []string
	}{
		{
			name:           "uses config defaults when no flag",
			configFormats:  []string{"json", "html"},
			expectedResult: []string{"json", "html"},
		},
		{
			name:           "uses flag when provided",
			configFormats:  []string{"json", "html"},
			flagValue:      []string{"junit"},
			setFlag:        true,
			expectedResult: []string{"junit"},
		},
		{
			name:           "defaults to json when no config or flag",
			configFormats:  []string{},
			expectedResult: []string{"json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test config
			cfg := &config.Config{
				Defaults: config.Defaults{
					OutputFormats: tt.configFormats,
					OutDir:        "test-out",
					Concurrency:   1,
				},
			}

			// Create test command with required flags
			cmd := &cobra.Command{}
			setupTestFlags(cmd)

			// Set format flag if specified
			if tt.setFlag {
				err := cmd.Flags().Set("format", formatSliceToString(tt.flagValue))
				require.NoError(t, err)
			}

			// Extract parameters
			params, err := extractRunParameters(cmd, cfg)
			require.NoError(t, err)

			// Verify output formats
			assert.Equal(t, tt.expectedResult, params.OutputFormats)
		})
	}
}

func TestEvalFiltering(t *testing.T) {
	tests := []struct {
		name          string
		flagValue     []string
		setFlag       bool
		expectedEvals []string
	}{
		{
			name:          "no eval flag",
			setFlag:       false,
			expectedEvals: []string{},
		},
		{
			name:          "single eval",
			flagValue:     []string{"basic-eval"},
			setFlag:       true,
			expectedEvals: []string{"basic-eval"},
		},
		{
			name:          "multiple evals",
			flagValue:     []string{"eval1", "eval2"},
			setFlag:       true,
			expectedEvals: []string{"eval1", "eval2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test config
			cfg := &config.Config{
				Defaults: config.Defaults{
					OutDir:      "test-out",
					Concurrency: 1,
				},
			}

			// Create test command
			cmd := &cobra.Command{}
			setupTestFlags(cmd)

			// Set eval flag if specified
			if tt.setFlag {
				err := cmd.Flags().Set("eval", formatSliceToString(tt.flagValue))
				require.NoError(t, err)
			}

			// Extract parameters
			params, err := extractRunParameters(cmd, cfg)
			require.NoError(t, err)

			// Verify evals
			assert.Equal(t, tt.expectedEvals, params.Evals)
		})
	}
}

func TestProcessDeprecatedHTMLFlag(t *testing.T) {
	tests := []struct {
		name              string
		htmlFlag          bool
		expectedHTML      bool
		expectedInFormats bool
	}{
		{
			name:              "html flag not set",
			htmlFlag:          false,
			expectedHTML:      false,
			expectedInFormats: false,
		},
		{
			name:              "html flag set",
			htmlFlag:          true,
			expectedHTML:      true,
			expectedInFormats: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			setupTestFlags(cmd)

			err := cmd.Flags().Set("html", fmt.Sprintf("%t", tt.htmlFlag))
			require.NoError(t, err)

			params := &RunParameters{
				OutputFormats: []string{},
			}

			err = processDeprecatedHTMLFlag(cmd, params)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedHTML, params.GenerateHTML)
			if tt.expectedInFormats {
				assert.Contains(t, params.OutputFormats, "html")
			} else {
				assert.NotContains(t, params.OutputFormats, "html")
			}
		})
	}
}

func TestProcessConfigHTMLSetting(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			HTMLReport: "/path/to/report.html",
		},
	}

	params := &RunParameters{
		OutputFormats: []string{},
	}

	processConfigHTMLSetting(cfg, params)

	assert.True(t, params.GenerateHTML)
	assert.Equal(t, "/path/to/report.html", params.HTMLReportPath)
	assert.Contains(t, params.OutputFormats, "html")
}

func TestApplyConfigurationOverrides(t *testing.T) {
	tests := []struct {
		name              string
		verbose           bool
		maxTokens         int
		setMaxTokens      bool
		expectedVerbose   bool
		expectedMaxTokens int
	}{
		{
			name:              "no overrides",
			verbose:           false,
			setMaxTokens:      false,
			expectedVerbose:   false,
			expectedMaxTokens: 0,
		},
		{
			name:              "verbose override",
			verbose:           true,
			setMaxTokens:      false,
			expectedVerbose:   true,
			expectedMaxTokens: 0,
		},
		{
			name:              "max tokens override",
			verbose:           false,
			maxTokens:         1000,
			setMaxTokens:      true,
			expectedVerbose:   false,
			expectedMaxTokens: 1000,
		},
		{
			name:              "both overrides",
			verbose:           true,
			maxTokens:         2000,
			setMaxTokens:      true,
			expectedVerbose:   true,
			expectedMaxTokens: 2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			setupTestFlags(cmd)

			cfg := &config.Config{
				Defaults: config.Defaults{},
			}

			params := &RunParameters{
				Verbose: tt.verbose,
			}

			if tt.setMaxTokens {
				err := cmd.Flags().Set("max-tokens", fmt.Sprintf("%d", tt.maxTokens))
				require.NoError(t, err)
			}

			applyConfigurationOverrides(cmd, cfg, params)

			assert.Equal(t, tt.expectedVerbose, cfg.Defaults.Verbose)
			assert.Equal(t, tt.expectedMaxTokens, cfg.Defaults.MaxTokens)
		})
	}
}

// Helper function to set up test flags (mirrors the real flags)
func setupTestFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("region", []string{}, "Regions to run")
	cmd.Flags().StringSlice("provider", []string{}, "Providers to use")
	cmd.Flags().StringSlice("scenario", []string{}, "Scenarios to run")
	cmd.Flags().StringSlice("eval", []string{}, "Evaluations to run")
	cmd.Flags().IntP("concurrency", "j", 6, "Number of concurrent workers")
	cmd.Flags().StringP("out", "o", "out", "Output directory")
	cmd.Flags().Bool("ci", false, "CI mode")
	cmd.Flags().Bool("simple", false, "Simple mode (alias for --ci)")
	cmd.Flags().Bool("verbose", false, "Verbose mode")
	cmd.Flags().Bool("mock-provider", false, "Use mock provider")
	cmd.Flags().String("mock-config", "", "Mock config file")
	cmd.Flags().StringSlice("format", []string{}, "Output formats")
	cmd.Flags().String("junit-file", "", "JUnit XML output file")
	cmd.Flags().String("html-file", "", "HTML report output file")
	cmd.Flags().String("markdown-file", "", "Markdown output file")
	cmd.Flags().Bool("html", false, "Generate HTML report")
	cmd.Flags().Float32("temperature", 0.6, "Temperature")
	cmd.Flags().Int("max-tokens", 0, "Max tokens")
	cmd.Flags().IntP("seed", "s", 42, "Random seed")
	cmd.Flags().Bool("skip-pack-evals", false, "Disable pack eval execution")
	cmd.Flags().StringSlice("eval-types", []string{}, "Filter to specific eval types")
}

// Helper function to convert slice to comma-separated string for flag setting
func formatSliceToString(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	result := slice[0]
	for i := 1; i < len(slice); i++ {
		result += "," + slice[i]
	}
	return result
}
