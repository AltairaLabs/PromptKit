package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// BenchmarkReport is the top-level result of a benchmark run.
type BenchmarkReport struct {
	Round     string       `json:"round"`
	Profile   string       `json:"profile"`
	Timestamp time.Time    `json:"timestamp"`
	Hardware  string       `json:"hardware"`
	Results   []TierResult `json:"results"`
}

// TierResult holds the metrics for one framework/concurrency tier.
type TierResult struct {
	Framework   string           `json:"framework"`
	Version     string           `json:"version"`
	Concurrency int              `json:"concurrency"`
	Summary     Summary          `json:"summary"`
	Resources   ResourceSnapshot `json:"resources"`
}

// WriteJSON marshals report as indented JSON and writes it to path.
func WriteJSON(report BenchmarkReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// RenderMarkdown returns a Markdown table summarising the benchmark results.
// Columns: Framework, Concurrent, p50, p99, Throughput, RSS
func RenderMarkdown(report BenchmarkReport) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Benchmark Report\n\n")
	fmt.Fprintf(&b, "- **Round:** %s\n", report.Round)
	fmt.Fprintf(&b, "- **Profile:** %s\n", report.Profile)
	fmt.Fprintf(&b, "- **Hardware:** %s\n", report.Hardware)
	fmt.Fprintf(&b, "- **Timestamp:** %s\n\n", report.Timestamp.Format(time.RFC3339))

	fmt.Fprintln(&b, "| Framework | Concurrent | p50 | p99 | Throughput | RSS |")
	fmt.Fprintln(&b, "|-----------|------------|-----|-----|------------|-----|")

	for _, r := range report.Results {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %.2f req/s | %.1f MB |\n",
			r.Framework,
			r.Concurrency,
			r.Summary.TotalP50.Round(time.Millisecond),
			r.Summary.TotalP99.Round(time.Millisecond),
			r.Summary.Throughput,
			r.Resources.PeakRSSMB,
		)
	}

	return b.String()
}

// WriteCSV writes one row per TierResult (plus a header) to path.
func WriteCSV(report BenchmarkReport, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)

	header := []string{
		"framework", "version", "concurrency",
		"count", "errors", "error_rate",
		"first_byte_p50_ms", "first_byte_p99_ms",
		"total_p50_ms", "total_p99_ms",
		"jitter_mean_ms", "throughput_rps",
		"wall_clock_ms",
		"peak_rss_mb", "avg_cpu_pct",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, r := range report.Results {
		s := r.Summary
		res := r.Resources
		row := []string{
			r.Framework,
			r.Version,
			strconv.Itoa(r.Concurrency),
			strconv.Itoa(s.Count),
			strconv.Itoa(s.Errors),
			strconv.FormatFloat(s.ErrorRate, 'f', 4, 64),
			strconv.FormatFloat(float64(s.FirstByteP50)/float64(time.Millisecond), 'f', 3, 64),
			strconv.FormatFloat(float64(s.FirstByteP99)/float64(time.Millisecond), 'f', 3, 64),
			strconv.FormatFloat(float64(s.TotalP50)/float64(time.Millisecond), 'f', 3, 64),
			strconv.FormatFloat(float64(s.TotalP99)/float64(time.Millisecond), 'f', 3, 64),
			strconv.FormatFloat(float64(s.JitterMean)/float64(time.Millisecond), 'f', 3, 64),
			strconv.FormatFloat(s.Throughput, 'f', 3, 64),
			strconv.FormatFloat(float64(s.WallClock)/float64(time.Millisecond), 'f', 3, 64),
			strconv.FormatFloat(res.PeakRSSMB, 'f', 2, 64),
			strconv.FormatFloat(res.AvgCPUPct, 'f', 2, 64),
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("write row for %s: %w", r.Framework, err)
		}
	}

	w.Flush()
	return w.Error()
}
