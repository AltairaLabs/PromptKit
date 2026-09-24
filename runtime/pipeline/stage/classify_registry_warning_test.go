package stage

import (
	"bytes"
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

func passthroughStage() Stage {
	return NewStageFunc("pass", StageTypeTransform, func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		for e := range in {
			out <- e
		}
		return nil
	})
}

// A host still setting the deprecated ClassifyRegistry gets no registry on the
// context, so inference-backed checks skip or deny. That must be loud, not
// silent.
func TestBuild_WarnsWhenOnlyTheDeprecatedClassifyRegistryIsSet(t *testing.T) {
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })

	cfg := DefaultPipelineConfig()
	cfg.ClassifyRegistry = classify.NewRegistry()
	_, err := NewPipelineBuilderWithConfig(cfg).Chain(passthroughStage()).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("ClassifyRegistry")) {
		t.Fatalf("expected a warning naming ClassifyRegistry, got log: %q", buf.String())
	}

	buf.Reset()
	cfg = DefaultPipelineConfig()
	cfg.ClassifyRegistry = classify.NewRegistry()
	cfg.InferenceRegistry = inference.NewRegistry()
	if _, err := NewPipelineBuilderWithConfig(cfg).Chain(passthroughStage()).Build(); err != nil {
		t.Fatalf("build: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("ClassifyRegistry")) {
		t.Fatalf("no warning when InferenceRegistry is set, got log: %q", buf.String())
	}
}
