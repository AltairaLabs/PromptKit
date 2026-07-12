// Package main demonstrates plugging a custom classify backend into the
// PromptKit SDK. It registers an ONNX wav2vec2 speech-emotion classifier
// via sdk.WithClassifier and scores the caller's audio with the built-in
// audio_emotion eval handler — no runtime changes, no HF token, offline.
//
// The native ONNX dependency lives entirely in this standalone example
// module, so the PromptKit runtime stays CGO-free/purego-free.
//
// Setup (one-time): `make setup` downloads libonnxruntime + the ONNX model.
// Run: `make run`  (or `GOWORK=off go run .`)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register built-in eval handlers (audio_emotion)
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// printHook prints every eval result the runner produces.
type printHook struct{}

func (printHook) Name() string { return "print" }

func (printHook) OnEvalResult(_ context.Context, _ *evals.EvalDef, _ *evals.EvalContext, r *evals.EvalResult) {
	switch {
	case r.Skipped:
		fmt.Printf("  [%s] skipped: %s\n", r.Type, r.SkipReason)
	case r.Error != "":
		fmt.Printf("  [%s] error: %s\n", r.Type, r.Error)
	default:
		score := 0.0
		if r.Score != nil {
			score = *r.Score
		}
		fmt.Printf("  [%s] %s (score=%.3f)\n", r.Type, r.Explanation, score)
	}
}

func main() {
	libPath := envOr("ONNX_LIB", filepath.Join("lib", "libonnxruntime."+libExt()))
	modelPath := envOr("ONNX_MODEL", filepath.Join("models", "model.onnx"))
	audioPath := envOr("AUDIO_FILE", filepath.Join("testdata", "sample-16k-mono.wav"))

	backend, err := newONNXAudioClassifier(onnxConfig{LibPath: libPath, ModelPath: modelPath})
	if err != nil {
		log.Fatalf("create ONNX backend: %v\nDid you run `make setup`?", err)
	}
	defer func() { _ = backend.Close() }()

	// Mock LLM so the demo needs no API key; the eval scores the caller's
	// audio (role: user), not the assistant's reply.
	repo := mock.NewInMemoryMockRepository(`{"response": "Thanks for calling — how can I help?"}`)
	provider := mock.NewProviderWithRepository("mock", "mock-model", false, repo)

	runner := evals.NewEvalRunner(evals.NewEvalTypeRegistry())

	conv, err := sdk.Open("./caller.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithClassifier("onnx-ser", backend), // <-- the pluggable seam
		sdk.WithEvalRunner(runner),
		sdk.WithEvalHook(printHook{}),
	)
	if err != nil {
		log.Fatalf("open conversation: %v", err)
	}
	defer conv.Close()

	fmt.Printf("Scoring caller audio: %s\n", audioPath)
	if _, err := conv.Send(context.Background(),
		"I have been on hold for an hour and I am furious.",
		sdk.WithAudioFile(audioPath),
	); err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Println("Done.")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func libExt() string {
	if _, err := os.Stat(filepath.Join("lib", "libonnxruntime.dylib")); err == nil {
		return "dylib"
	}
	return "so"
}
