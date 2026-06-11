package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

func TestPipeline_AttachesClassifyRegistry(t *testing.T) {
	reg := classify.NewRegistry()

	var seen *classify.Registry
	probe := NewStageFunc("probe", StageTypeTransform, func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		seen = classify.FromContext(ctx)
		for e := range in {
			out <- e
		}
		return nil
	})

	cfg := DefaultPipelineConfig()
	cfg.ClassifyRegistry = reg
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(probe).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	in := make(chan StreamElement)
	out, err := p.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	close(in)
	for range out { //nolint:revive // drain
	}

	if seen != reg {
		t.Fatalf("stage did not observe the configured classify registry (got %v)", seen)
	}
}
