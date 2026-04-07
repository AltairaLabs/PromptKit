package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func main() {
	round := flag.String("round", "", "benchmark round: round1 (streaming) or round2 (voice)")
	targetURL := flag.String("target", "", "target framework URL")
	framework := flag.String("framework", "", "framework name (for reporting)")
	version := flag.String("version", "", "framework version (for reporting)")
	concurrency := flag.Int("concurrency", 100, "concurrent connections")
	requests := flag.Int("requests", 1000, "total requests (round1) or sessions (round2)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	outputDir := flag.String("output", "results", "output directory for results")
	profile := flag.String("profile", "default", "profile name (for reporting)")
	frameworkPID := flag.Int("framework-pid", 0, "PID of framework process to monitor (0 = skip)")
	flag.Parse()

	if *round == "" || *targetURL == "" || *framework == "" {
		flag.Usage()
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var sampler *ResourceSampler
	if *frameworkPID > 0 {
		sampler = NewResourceSampler(*frameworkPID, 100*time.Millisecond)
		sampler.Start()
	}

	var agg *Aggregator
	var err error

	switch *round {
	case "round1":
		cfg := StreamingConfig{
			TargetURL:   *targetURL,
			Concurrency: *concurrency,
			Requests:    *requests,
			Timeout:     *timeout,
			Prompt:      "Write a short paragraph about the history of computing.",
		}
		log.Printf("round1: %s @ %d concurrent, %d requests", *framework, *concurrency, *requests)
		agg, err = RunStreamingBenchmark(ctx, cfg)

	case "round2":
		cfg := VoiceConfig{
			TargetURL:      *targetURL,
			Concurrency:    *concurrency,
			Sessions:       *requests,
			AudioFrames:    50,
			FrameSize:      640,
			FrameInterval:  20 * time.Millisecond,
			SessionTimeout: *timeout,
		}
		log.Printf("round2: %s @ %d concurrent, %d sessions", *framework, *concurrency, *requests)
		agg, err = RunVoiceBenchmark(ctx, cfg)

	default:
		log.Fatalf("unknown round: %s", *round)
	}

	if err != nil {
		log.Fatalf("benchmark failed: %v", err)
	}

	summary := agg.Summarize()
	summary.Concurrency = *concurrency

	var resources ResourceSnapshot
	if sampler != nil {
		resources = sampler.Stop()
	}

	report := BenchmarkReport{
		Round:     *round,
		Profile:   *profile,
		Timestamp: time.Now().UTC(),
		Hardware:  fmt.Sprintf("%s/%s %d cores", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		Results: []TierResult{{
			Framework:   *framework,
			Version:     *version,
			Concurrency: *concurrency,
			Summary:     summary,
			Resources:   resources,
		}},
	}

	os.MkdirAll(*outputDir, 0o755)
	ts := time.Now().Format("20060102-150405")
	base := fmt.Sprintf("%s-%s-%s", ts, *round, *framework)

	jsonPath := filepath.Join(*outputDir, base+".json")
	if err := WriteJSON(report, jsonPath); err != nil {
		log.Fatalf("write JSON: %v", err)
	}

	csvPath := filepath.Join(*outputDir, base+".csv")
	if err := WriteCSV(report, csvPath); err != nil {
		log.Fatalf("write CSV: %v", err)
	}

	fmt.Println(RenderMarkdown(report))
	log.Printf("results written to %s and %s", jsonPath, csvPath)
}
