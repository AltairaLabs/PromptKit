package main

import (
	"math"
	"testing"
	"time"
)

func TestRequestMetrics_Percentiles(t *testing.T) {
	a := &Aggregator{}
	for i := 1; i <= 100; i++ {
		a.Record(RequestResult{
			FirstByteLatency: time.Duration(i) * time.Millisecond,
			TotalDuration:    time.Duration(i) * time.Millisecond,
		})
	}
	s := a.Summarize()

	if s.Count != 100 {
		t.Errorf("expected count 100, got %d", s.Count)
	}
	if s.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", s.Errors)
	}

	// p50 of 1..100ms → 50ms or 51ms (interpolation)
	if s.FirstByteP50 < 49*time.Millisecond || s.FirstByteP50 > 51*time.Millisecond {
		t.Errorf("expected p50 ≈ 50ms, got %v", s.FirstByteP50)
	}
	// p99 of 1..100ms → 99ms
	if s.FirstByteP99 < 98*time.Millisecond || s.FirstByteP99 > 100*time.Millisecond {
		t.Errorf("expected p99 ≈ 99ms, got %v", s.FirstByteP99)
	}
}

func TestJitterCalculation(t *testing.T) {
	intervals := []time.Duration{
		10 * time.Millisecond,
		12 * time.Millisecond,
		8 * time.Millisecond,
		11 * time.Millisecond,
		9 * time.Millisecond,
	}
	// mean = 10ms, stddev ≈ 1.4ms
	jitter := calcJitter(intervals)
	expected := 1.4 * float64(time.Millisecond)
	actual := float64(jitter)
	if math.Abs(actual-expected)/expected > 0.2 {
		t.Errorf("expected jitter ≈ 1.4ms, got %v", jitter)
	}
}

func TestAggregator_ErrorRate(t *testing.T) {
	a := &Aggregator{}
	for i := 0; i < 8; i++ {
		a.Record(RequestResult{
			FirstByteLatency: 10 * time.Millisecond,
			TotalDuration:    20 * time.Millisecond,
		})
	}
	for i := 0; i < 2; i++ {
		a.Record(RequestResult{
			Error: errTest,
		})
	}
	s := a.Summarize()
	if s.Count != 10 {
		t.Errorf("expected count 10, got %d", s.Count)
	}
	if s.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", s.Errors)
	}
	if math.Abs(s.ErrorRate-0.2) > 0.001 {
		t.Errorf("expected error rate 0.2, got %f", s.ErrorRate)
	}
}

func TestPercentile_EdgeCases(t *testing.T) {
	single := []time.Duration{42 * time.Millisecond}
	if percentile(single, 50) != 42*time.Millisecond {
		t.Errorf("single-element p50 should be 42ms")
	}
	if percentile(single, 99) != 42*time.Millisecond {
		t.Errorf("single-element p99 should be 42ms")
	}

	empty := []time.Duration{}
	if percentile(empty, 50) != 0 {
		t.Errorf("empty slice p50 should be 0")
	}
}

func TestCalcJitter_Empty(t *testing.T) {
	if calcJitter(nil) != 0 {
		t.Error("nil intervals should return 0")
	}
	if calcJitter([]time.Duration{}) != 0 {
		t.Error("empty intervals should return 0")
	}
	if calcJitter([]time.Duration{5 * time.Millisecond}) != 0 {
		t.Error("single interval should return 0 jitter")
	}
}
