package main

import (
	"os"
	"testing"
	"time"
)

func TestResourceSampler_CapturesSelf(t *testing.T) {
	pid := os.Getpid()
	sampler := NewResourceSampler(pid, 50*time.Millisecond)
	sampler.Start()
	time.Sleep(200 * time.Millisecond)
	snap := sampler.Stop()

	if snap.PeakRSSMB <= 0 {
		t.Errorf("expected PeakRSSMB > 0, got %f", snap.PeakRSSMB)
	}
	if snap.Samples <= 0 {
		t.Errorf("expected Samples > 0, got %d", snap.Samples)
	}
}
