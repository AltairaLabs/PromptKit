package main

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
)

// TestHTMLReportPathResolution tests how HTML report paths should be resolved
// using the new simplified logic where html_report is always relative to out_dir
func TestHTMLReportPathResolution(t *testing.T) {
	tests := []struct {
		name           string
		htmlReportPath string
		outDir         string
		want           string
	}{
		{
			name:           "filename should be placed in outDir",
			htmlReportPath: "report.html",
			outDir:         "out",
			want:           "out/report.html",
		},
		{
			name:           "filename with subdir should be placed in outDir",
			htmlReportPath: "reports/summary.html",
			outDir:         "out",
			want:           "out/reports/summary.html",
		},
		{
			name:           "absolute path should be used as-is",
			htmlReportPath: "/tmp/report.html",
			outDir:         "out",
			want:           "/tmp/report.html",
		},
		{
			name:           "empty should return empty",
			htmlReportPath: "",
			outDir:         "out",
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveOutputPath(tt.outDir, tt.htmlReportPath)
			if got != tt.want {
				t.Errorf("ResolveOutputPath(%q, %q) = %q, want %q",
					tt.outDir, tt.htmlReportPath, got, tt.want)
			}
		})
	}
}
