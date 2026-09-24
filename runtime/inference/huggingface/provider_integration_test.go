//go:build integration

// Live Hugging Face inference checks against the serverless router.
//
//	HF_TOKEN=... go test -tags=integration ./runtime/inference/huggingface/... -v
package huggingface

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestLiveRouterTasks(t *testing.T) {
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		t.Skip("HF_TOKEN not set")
	}
	p, err := New(Config{APIKey: token})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		req  inference.Request
		top  string
	}{
		{"sentiment", inference.Request{Model: "distilbert/distilbert-base-uncased-finetuned-sst-2-english",
			Inputs: []types.Message{{Role: "user", Content: "I love this product"}}}, "POSITIVE"},
		{"toxicity multi-label", inference.Request{Model: "unitary/toxic-bert",
			Inputs: []types.Message{{Role: "assistant", Content: "you are an idiot"}},
			Params: map[string]any{"multi_label": true}}, "toxic"},
		{"zero-shot", inference.Request{Model: "facebook/bart-large-mnli",
			Inputs: []types.Message{{Role: "user", Content: "Which stocks should I buy?"}},
			Labels: []string{"banking", "investing", "cooking"}}, "investing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := p.Infer(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Scores) == 0 || resp.Scores[0].Label != c.top {
				t.Fatalf("top label = %v, want %s", resp.Scores, c.top)
			}
		})
	}
}
