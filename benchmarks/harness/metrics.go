package main

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"
)

// errTest is a sentinel error used in tests.
var errTest = errors.New("test error")

// RequestResult captures timing data for a single request.
type RequestResult struct {
	FirstByteLatency time.Duration
	TotalDuration    time.Duration
	ChunkCount       int
	ChunkIntervals   []time.Duration
	Error            error
}

// Summary holds aggregated metrics for a benchmark run at one concurrency tier.
type Summary struct {
	Concurrency  int
	Count        int
	Errors       int
	ErrorRate    float64
	FirstByteP50 time.Duration
	FirstByteP99 time.Duration
	TotalP50     time.Duration
	TotalP99     time.Duration
	JitterMean   time.Duration
	Throughput   float64
	WallClock    time.Duration
}

// Aggregator collects request results and computes summary statistics. Thread-safe.
type Aggregator struct {
	mu      sync.Mutex
	results []RequestResult
}

// Record appends a result in a concurrent-safe way.
func (a *Aggregator) Record(r RequestResult) {
	a.mu.Lock()
	a.results = append(a.results, r)
	a.mu.Unlock()
}

// Summarize computes percentiles and other statistics over all recorded results.
func (a *Aggregator) Summarize() Summary {
	a.mu.Lock()
	snapshot := make([]RequestResult, len(a.results))
	copy(snapshot, a.results)
	a.mu.Unlock()

	s := Summary{Count: len(snapshot)}

	var firstBytes []time.Duration
	var totals []time.Duration
	var allIntervals []time.Duration

	for _, r := range snapshot {
		if r.Error != nil {
			s.Errors++
			continue
		}
		firstBytes = append(firstBytes, r.FirstByteLatency)
		totals = append(totals, r.TotalDuration)
		allIntervals = append(allIntervals, r.ChunkIntervals...)
	}

	if s.Count > 0 {
		s.ErrorRate = float64(s.Errors) / float64(s.Count)
	}

	sort.Slice(firstBytes, func(i, j int) bool { return firstBytes[i] < firstBytes[j] })
	sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })

	s.FirstByteP50 = percentile(firstBytes, 50)
	s.FirstByteP99 = percentile(firstBytes, 99)
	s.TotalP50 = percentile(totals, 50)
	s.TotalP99 = percentile(totals, 99)
	s.JitterMean = calcJitter(allIntervals)

	return s
}

// percentile returns the p-th percentile of a sorted slice of durations.
// p should be in [0, 100]. Returns 0 for an empty slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	// Use nearest-rank method.
	rank := (p / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	interpolated := float64(sorted[lower]) + frac*(float64(sorted[upper])-float64(sorted[lower]))
	return time.Duration(interpolated)
}

// calcJitter returns the standard deviation of chunk intervals.
// Returns 0 for nil, empty, or single-element slices.
func calcJitter(intervals []time.Duration) time.Duration {
	if len(intervals) <= 1 {
		return 0
	}

	var sum float64
	for _, iv := range intervals {
		sum += float64(iv)
	}
	mean := sum / float64(len(intervals))

	var variance float64
	for _, iv := range intervals {
		diff := float64(iv) - mean
		variance += diff * diff
	}
	variance /= float64(len(intervals))

	return time.Duration(math.Sqrt(variance))
}
