package config

import (
	"path/filepath"
	"testing"
)

// TestResolveOutputPath tests the logic for resolving output paths
// relative to the configured output directory
func TestResolveOutputPath(t *testing.T) {
	tests := []struct {
		name     string
		outDir   string
		filename string
		expected string
	}{
		{
			name:     "simple filename in out directory",
			outDir:   "out",
			filename: "report.html",
			expected: "out/report.html",
		},
		{
			name:     "filename with subdirectory in out directory",
			outDir:   "out",
			filename: "reports/summary.html",
			expected: "out/reports/summary.html",
		},
		{
			name:     "absolute path should use as-is",
			outDir:   "out",
			filename: "/tmp/report.html",
			expected: "/tmp/report.html",
		},
		{
			name:     "empty filename should return empty",
			outDir:   "out",
			filename: "",
			expected: "",
		},
		{
			name:     "out_dir with trailing slash",
			outDir:   "out/",
			filename: "report.html",
			expected: "out/report.html",
		},
		{
			name:     "nested out_dir",
			outDir:   "output/results",
			filename: "report.html",
			expected: "output/results/report.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveOutputPath(tt.outDir, tt.filename)
			if result != tt.expected {
				t.Errorf("ResolveOutputPath(%q, %q) = %q, expected %q",
					tt.outDir, tt.filename, result, tt.expected)
			}
		})
	}
}

// TestResolveOutputPath_NormalizesPath tests that paths are properly normalized
func TestResolveOutputPath_NormalizesPath(t *testing.T) {
	result := ResolveOutputPath("out", "report.html")
	expected := filepath.Join("out", "report.html")

	if result != expected {
		t.Errorf("Expected normalized path %q, got %q", expected, result)
	}
}

// TestResolveOutputPath_RealWorldExamples tests real-world usage scenarios
func TestResolveOutputPath_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name        string
		outDir      string
		filename    string
		expected    string
		description string
	}{
		{
			name:        "default config",
			outDir:      "out",
			filename:    "report.html",
			expected:    "out/report.html",
			description: "Simple filename in default out directory",
		},
		{
			name:        "custom output dir",
			outDir:      "custom_output",
			filename:    "report.html",
			expected:    "custom_output/report.html",
			description: "CLI override of output directory",
		},
		{
			name:        "nested output structure",
			outDir:      "results/2025-10",
			filename:    "summary.html",
			expected:    "results/2025-10/summary.html",
			description: "Nested output directory structure",
		},
		{
			name:        "report with subdirectory",
			outDir:      "out",
			filename:    "reports/detailed.html",
			expected:    "out/reports/detailed.html",
			description: "HTML report in subdirectory of out_dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveOutputPath(tt.outDir, tt.filename)
			if result != tt.expected {
				t.Errorf("%s: ResolveOutputPath(%q, %q) = %q, expected %q",
					tt.description, tt.outDir, tt.filename, result, tt.expected)
			}
		})
	}
}
