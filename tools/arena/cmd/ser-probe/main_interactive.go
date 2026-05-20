// Command ser-probe exercises the runtime/classify HF backend directly
// against an audio file or text string, so you can verify the inference
// pipeline works against your HF_TOKEN without running the full arena
// scenario (which depends on duplex providers behaving correctly).
//
// Usage:
//
//	HF_TOKEN=... promptarena-ser-probe \
//	  -audio examples/voice-refund-demo/out/media/runs/<id>/<hash>.wav \
//	  -model superb/wav2vec2-base-superb-er \
//	  -label angry
//
//	HF_TOKEN=... promptarena-ser-probe \
//	  -text "I want a refund and I want it NOW" \
//	  -model SamLowe/roberta-base-go_emotions \
//	  -label anger
//
// HF retired most audio-classification models from their free serverless
// tier in early 2026. If -audio returns "Model not supported by provider
// hf-inference", point -model at one that IS still warm or use a paid HF
// Inference Endpoint via -base-url.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
)

// Standard exit codes per BSD sysexits convention.
const (
	exitRuntimeError = 1
	exitUsage        = 2
)

func main() {
	audioPath := flag.String("audio", "", "path to a local .wav file")
	textInput := flag.String("text", "", "text to classify (use instead of -audio for text models)")
	model := flag.String("model", "SamLowe/roberta-base-go_emotions", "HF model id")
	label := flag.String("label", "anger", "label to look up in the model's response")
	baseURL := flag.String("base-url", "", "override HF base URL (e.g. a dedicated Inference Endpoint)")
	flag.Parse()

	token := firstNonEmpty(os.Getenv("HF_TOKEN"), os.Getenv("HUGGING_FACE_HUB_TOKEN"))
	if token == "" {
		fmt.Fprintln(os.Stderr, "HF_TOKEN (or HUGGING_FACE_HUB_TOKEN) must be set")
		os.Exit(exitUsage)
	}

	cfg := classifyhf.Config{APIKey: token}
	if *baseURL != "" {
		cfg.BaseURL = *baseURL
	}
	client, err := classifyhf.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hf client: %v\n", err)
		os.Exit(exitRuntimeError)
	}

	ctx := context.Background()
	var scores []classify.LabelScore
	switch {
	case *audioPath != "":
		body, readErr := os.ReadFile(*audioPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "read audio: %v\n", readErr)
			os.Exit(exitRuntimeError)
		}
		scores, err = client.ClassifyAudio(ctx, body, classify.AudioOptions{
			Model:    *model,
			MIMEType: "audio/wav",
		})
	case *textInput != "":
		scores, err = client.ClassifyText(ctx, *textInput, classify.TextOptions{
			Model:      *model,
			MultiLabel: true,
		})
	default:
		fmt.Fprintln(os.Stderr, "provide -audio or -text")
		os.Exit(exitUsage)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "classify: %v\n", err)
		os.Exit(exitRuntimeError)
	}

	fmt.Println("Top 5 labels:")
	maxRows := 5
	if len(scores) < maxRows {
		maxRows = len(scores)
	}
	for i := 0; i < maxRows; i++ {
		fmt.Printf("  %-20s %.4f\n", scores[i].Label, scores[i].Score)
	}
	for i := range scores {
		if scores[i].Label == *label {
			fmt.Printf("\n%s score: %.4f\n", *label, scores[i].Score)
			return
		}
	}
	fmt.Printf("\n%q not in returned labels\n", *label)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
