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

// c6g.xlarge reference instance for cost calculations.
const (
	refInstanceName   = "c6g.xlarge"
	refInstanceCPUPct = 400.0  // 4 vCPUs = 400%
	refInstanceRAMMB  = 8192.0 // 8 GB
	refInstanceCostHr = 0.136  // USD on-demand
)

// CostSummary holds derived cost metrics for one framework/tier result.
type CostSummary struct {
	InstancesPerBox   int     `json:"instances_per_box"`
	AggregateRPS      float64 `json:"aggregate_rps"`
	CostPerMillionReq float64 `json:"cost_per_million_requests"`
}

// ComputeCost derives how many instances fit on the reference instance
// and the resulting cost per million requests.
func ComputeCost(rps float64, peakRSSMB float64, avgCPUPct float64) CostSummary {
	if rps <= 0 || peakRSSMB <= 0 || avgCPUPct <= 0 {
		return CostSummary{}
	}

	byRAM := int(refInstanceRAMMB / peakRSSMB)
	byCPU := int(refInstanceCPUPct / avgCPUPct)

	instances := byRAM
	if byCPU < instances {
		instances = byCPU
	}
	if instances < 1 {
		instances = 1
	}

	aggRPS := float64(instances) * rps
	costPerMillion := (refInstanceCostHr / aggRPS) * (1_000_000.0 / 3600.0)

	return CostSummary{
		InstancesPerBox:   instances,
		AggregateRPS:      aggRPS,
		CostPerMillionReq: costPerMillion,
	}
}

// RenderCostMarkdown returns a Markdown cost comparison table.
func RenderCostMarkdown(report BenchmarkReport) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Cost Summary (%s: %d vCPU, %.0f GB, $%.3f/hr)\n\n",
		refInstanceName, int(refInstanceCPUPct/100), refInstanceRAMMB/1024, refInstanceCostHr)

	fmt.Fprintln(&b, "| Framework | Concurrent | rps | Peak RSS | Avg CPU | Instances/box | Agg rps | $/M reqs |")
	fmt.Fprintln(&b, "|-----------|------------|-----|----------|---------|---------------|---------|----------|")

	for _, r := range report.Results {
		cost := ComputeCost(r.Summary.Throughput, r.Resources.PeakRSSMB, r.Resources.AvgCPUPct)
		fmt.Fprintf(&b, "| %s | %d | %.0f | %.0f MB | %.1f%% | %d | %.0f | $%.2f |\n",
			r.Framework,
			r.Concurrency,
			r.Summary.Throughput,
			r.Resources.PeakRSSMB,
			r.Resources.AvgCPUPct,
			cost.InstancesPerBox,
			cost.AggregateRPS,
			cost.CostPerMillionReq,
		)
	}

	return b.String()
}
