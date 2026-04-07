package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleReport() BenchmarkReport {
	return BenchmarkReport{
		Round:     "round-1",
		Profile:   "default",
		Timestamp: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		Hardware:  "MacBook Pro M3",
		Results: []TierResult{
			{
				Framework:   "promptkit",
				Version:     "v1.0.0",
				Concurrency: 10,
				Summary: Summary{
					Concurrency:  10,
					Count:        100,
					Errors:       2,
					ErrorRate:    0.02,
					FirstByteP50: 120 * time.Millisecond,
					FirstByteP99: 450 * time.Millisecond,
					TotalP50:     500 * time.Millisecond,
					TotalP99:     1200 * time.Millisecond,
					JitterMean:   15 * time.Millisecond,
					Throughput:   12.5,
					WallClock:    8 * time.Second,
				},
				Resources: ResourceSnapshot{
					PeakRSSMB: 128.5,
					AvgCPUPct: 45.2,
					Samples:   80,
				},
			},
			{
				Framework:   "baseline",
				Version:     "v0.9.0",
				Concurrency: 5,
				Summary: Summary{
					Concurrency:  5,
					Count:        50,
					Errors:       0,
					ErrorRate:    0.0,
					FirstByteP50: 200 * time.Millisecond,
					FirstByteP99: 600 * time.Millisecond,
					TotalP50:     800 * time.Millisecond,
					TotalP99:     2000 * time.Millisecond,
					JitterMean:   25 * time.Millisecond,
					Throughput:   6.0,
					WallClock:    10 * time.Second,
				},
				Resources: ResourceSnapshot{
					PeakRSSMB: 64.0,
					AvgCPUPct: 22.1,
					Samples:   100,
				},
			},
		},
	}
}

func TestReporter_JSON(t *testing.T) {
	report := sampleReport()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	if err := WriteJSON(report, path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got BenchmarkReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(got.Results) != len(report.Results) {
		t.Fatalf("results len: got %d, want %d", len(got.Results), len(report.Results))
	}
	if got.Results[0].Framework != "promptkit" {
		t.Errorf("framework[0]: got %q, want %q", got.Results[0].Framework, "promptkit")
	}
	if got.Results[1].Framework != "baseline" {
		t.Errorf("framework[1]: got %q, want %q", got.Results[1].Framework, "baseline")
	}
	if got.Round != report.Round {
		t.Errorf("round: got %q, want %q", got.Round, report.Round)
	}
	if got.Profile != report.Profile {
		t.Errorf("profile: got %q, want %q", got.Profile, report.Profile)
	}
}

func TestReporter_Markdown(t *testing.T) {
	report := sampleReport()
	md := RenderMarkdown(report)

	// Must contain table pipes
	if !strings.Contains(md, "|") {
		t.Error("expected markdown table pipes '|'")
	}
	// Must contain framework names
	if !strings.Contains(md, "promptkit") {
		t.Error("expected framework name 'promptkit' in markdown output")
	}
	if !strings.Contains(md, "baseline") {
		t.Error("expected framework name 'baseline' in markdown output")
	}
	// Must contain column headers
	for _, header := range []string{"Framework", "Concurrent", "p50", "p99", "Throughput", "RSS"} {
		if !strings.Contains(md, header) {
			t.Errorf("expected column header %q in markdown output", header)
		}
	}
}

func TestReporter_CSV(t *testing.T) {
	report := sampleReport()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.csv")

	if err := WriteCSV(report, path); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	// Header + one row per result
	if len(lines) != 1+len(report.Results) {
		t.Fatalf("lines: got %d, want %d (header + %d results)", len(lines), 1+len(report.Results), len(report.Results))
	}
	if !strings.Contains(lines[0], "framework") {
		t.Errorf("header line missing 'framework': %q", lines[0])
	}
	if !strings.Contains(content, "promptkit") {
		t.Error("expected 'promptkit' in CSV output")
	}
	if !strings.Contains(content, "baseline") {
		t.Error("expected 'baseline' in CSV output")
	}
}
