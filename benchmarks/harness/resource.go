package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResourceSnapshot holds the aggregated resource usage collected by ResourceSampler.
type ResourceSnapshot struct {
	PeakRSSMB float64
	AvgCPUPct float64
	Samples   int
}

// ResourceSampler polls ps at a fixed interval to track RSS and CPU usage for a process.
type ResourceSampler struct {
	pid      int
	interval time.Duration

	mu        sync.Mutex
	peakRSSKB float64
	totalCPU  float64
	samples   int

	stop chan struct{}
	done chan struct{}
}

// NewResourceSampler creates a ResourceSampler for the given pid and polling interval.
func NewResourceSampler(pid int, interval time.Duration) *ResourceSampler {
	return &ResourceSampler{
		pid:      pid,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the background sampling goroutine.
func (s *ResourceSampler) Start() {
	go s.run()
}

// Stop halts sampling and returns the aggregated snapshot.
func (s *ResourceSampler) Stop() ResourceSnapshot {
	close(s.stop)
	<-s.done

	s.mu.Lock()
	defer s.mu.Unlock()

	snap := ResourceSnapshot{
		PeakRSSMB: s.peakRSSKB / 1024.0,
		Samples:   s.samples,
	}
	if s.samples > 0 {
		snap.AvgCPUPct = s.totalCPU / float64(s.samples)
	}
	return snap
}

func (s *ResourceSampler) run() {
	defer close(s.done)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			rssKB, cpuPct, err := samplePS(s.pid)
			if err != nil {
				continue
			}
			s.mu.Lock()
			if rssKB > s.peakRSSKB {
				s.peakRSSKB = rssKB
			}
			s.totalCPU += cpuPct
			s.samples++
			s.mu.Unlock()
		}
	}
}

// samplePS runs ps and returns RSS in KB and CPU %.
func samplePS(pid int) (rssKB float64, cpuPct float64, err error) {
	var out bytes.Buffer
	cmd := exec.Command("ps", "-o", "rss=,pcpu=", "-p", fmt.Sprintf("%d", pid))
	cmd.Stdout = &out
	if err = cmd.Run(); err != nil {
		return 0, 0, err
	}

	fields := strings.Fields(strings.TrimSpace(out.String()))
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("unexpected ps output: %q", out.String())
	}

	rssKB, err = strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse rss %q: %w", fields[0], err)
	}
	cpuPct, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse pcpu %q: %w", fields[1], err)
	}
	return rssKB, cpuPct, nil
}
