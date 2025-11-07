package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIFormatOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an arena.yaml with default formats
	arenaYAML := `apiVersion: "promptkit.altairalabs.ai/v1"
kind: "Arena"
metadata:
  name: "cli-override-test"
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1000
    output:
      dir: "out"
      formats: ["json", "html"]  # Default formats from config
      html:
        file: "report.html"`

	configFile := filepath.Join(tmpDir, "arena.yaml")
	err := os.WriteFile(configFile, []byte(arenaYAML), 0644)
	require.NoError(t, err)

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Test 1: No CLI flags - should use config defaults
	t.Run("no_cli_flags_uses_config_defaults", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringSlice("format", []string{}, "Output formats")
		cmd.Flags().StringSlice("formats", []string{}, "Output formats")
		cmd.Flags().String("junit-file", "", "JUnit XML output file")
		cmd.Flags().String("html-file", "", "HTML output file")
		cmd.Flags().String("markdown-file", "", "Markdown output file")

		params := &RunParameters{}
		err := extractOutputFormatFlags(cmd, cfg, params)
		require.NoError(t, err)

		// Should use config defaults
		assert.Equal(t, []string{"json", "html"}, params.OutputFormats)
	})

	// Test 2: CLI --format flag should override config
	t.Run("cli_format_overrides_config", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringSlice("format", []string{}, "Output formats")
		cmd.Flags().StringSlice("formats", []string{}, "Output formats")
		cmd.Flags().String("junit-file", "", "JUnit XML output file")
		cmd.Flags().String("html-file", "", "HTML output file")
		cmd.Flags().String("markdown-file", "", "Markdown output file")

		// Simulate CLI flag being set
		err := cmd.Flags().Set("format", "markdown")
		require.NoError(t, err)
		err = cmd.Flags().Set("format", "junit")
		require.NoError(t, err)

		params := &RunParameters{}
		err = extractOutputFormatFlags(cmd, cfg, params)
		require.NoError(t, err)

		// Should override config with CLI values
		assert.Equal(t, []string{"markdown", "junit"}, params.OutputFormats)
	})

	// Test 3: CLI --formats flag should override config
	t.Run("cli_formats_overrides_config", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringSlice("format", []string{}, "Output formats")
		cmd.Flags().StringSlice("formats", []string{}, "Output formats")
		cmd.Flags().String("junit-file", "", "JUnit XML output file")
		cmd.Flags().String("html-file", "", "HTML output file")
		cmd.Flags().String("markdown-file", "", "Markdown output file")

		// Simulate CLI flag being set
		err := cmd.Flags().Set("formats", "markdown")
		require.NoError(t, err)

		params := &RunParameters{}
		err = extractOutputFormatFlags(cmd, cfg, params)
		require.NoError(t, err)

		// Should override config with CLI values
		assert.Equal(t, []string{"markdown"}, params.OutputFormats)
	})
}

func TestBackwardCompatibilityFormatOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an arena.yaml with old format
	arenaYAML := `apiVersion: "promptkit.altairalabs.ai/v1"
kind: "Arena"
metadata:
  name: "backward-compat-test"
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1000
    out_dir: "legacy-out"
    html_report: "legacy-report.html"
    output_formats: ["json", "html"]  # Old format`

	configFile := filepath.Join(tmpDir, "arena.yaml")
	err := os.WriteFile(configFile, []byte(arenaYAML), 0644)
	require.NoError(t, err)

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Test that CLI still overrides even with old config format
	t.Run("cli_overrides_backward_compat_config", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringSlice("format", []string{}, "Output formats")
		cmd.Flags().StringSlice("formats", []string{}, "Output formats")
		cmd.Flags().String("junit-file", "", "JUnit XML output file")
		cmd.Flags().String("html-file", "", "HTML output file")
		cmd.Flags().String("markdown-file", "", "Markdown output file")

		// Simulate CLI flag being set
		err := cmd.Flags().Set("format", "markdown")
		require.NoError(t, err)

		params := &RunParameters{}
		err = extractOutputFormatFlags(cmd, cfg, params)
		require.NoError(t, err)

		// Should override config with CLI values
		assert.Equal(t, []string{"markdown"}, params.OutputFormats)
	})

	// Test that defaults work with backward compatibility
	t.Run("backward_compat_defaults_work", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringSlice("format", []string{}, "Output formats")
		cmd.Flags().StringSlice("formats", []string{}, "Output formats")
		cmd.Flags().String("junit-file", "", "JUnit XML output file")
		cmd.Flags().String("html-file", "", "HTML output file")
		cmd.Flags().String("markdown-file", "", "Markdown output file")

		params := &RunParameters{}
		err := extractOutputFormatFlags(cmd, cfg, params)
		require.NoError(t, err)

		// Should use backward compatible config defaults
		assert.Equal(t, []string{"json", "html"}, params.OutputFormats)
	})
}
