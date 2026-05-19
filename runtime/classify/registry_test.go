package classify

import (
	"context"
	"strings"
	"testing"
)

// stubAudio is an AudioClassifier that records the last call. Used to
// confirm the registry returns the actual registered instance, not a
// wrapper or copy.
type stubAudio struct {
	id string
}

func (s *stubAudio) ClassifyAudio(_ context.Context, _ []byte, _ AudioOptions) ([]LabelScore, error) {
	return []LabelScore{{Label: s.id, Score: 1.0}}, nil
}

type stubText struct{ id string }

func (s *stubText) ClassifyText(_ context.Context, _ string, _ TextOptions) ([]LabelScore, error) {
	return []LabelScore{{Label: s.id, Score: 1.0}}, nil
}

type stubImage struct{ id string }

func (s *stubImage) ClassifyImage(_ context.Context, _ []byte, _ ImageOptions) ([]LabelScore, error) {
	return []LabelScore{{Label: s.id, Score: 1.0}}, nil
}

type stubVideo struct{ id string }

func (s *stubVideo) ClassifyVideo(_ context.Context, _ []byte, _ VideoOptions) ([]LabelScore, error) {
	return []LabelScore{{Label: s.id, Score: 1.0}}, nil
}

type stubEmbedder struct{ id string }

func (s *stubEmbedder) Embed(_ context.Context, inputs []string, _ EmbedOptions) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = []float32{1.0}
	}
	return out, nil
}

func TestRegistry_EmptyByDefault(t *testing.T) {
	r := NewRegistry()
	if _, err := r.AudioClassifier(""); err == nil {
		t.Error("AudioClassifier on empty registry must return error")
	}
	if _, err := r.AudioClassifier("hf"); err == nil {
		t.Error("AudioClassifier lookup of unregistered id must return error")
	}
}

func TestRegistry_AudioRoundtrip(t *testing.T) {
	r := NewRegistry()
	r.RegisterAudio("hf", &stubAudio{id: "hf"})

	got, err := r.AudioClassifier("hf")
	if err != nil {
		t.Fatalf("AudioClassifier: %v", err)
	}
	scores, err := got.ClassifyAudio(context.Background(), nil, AudioOptions{})
	if err != nil {
		t.Fatalf("ClassifyAudio: %v", err)
	}
	if len(scores) != 1 || scores[0].Label != "hf" {
		t.Errorf("registry returned wrong instance; got %v", scores)
	}
}

func TestRegistry_DefaultFallback(t *testing.T) {
	r := NewRegistry()
	r.RegisterAudio("hf", &stubAudio{id: "hf"})
	r.RegisterAudio("onnx", &stubAudio{id: "onnx"})
	if err := r.SetDefaultAudio("hf"); err != nil {
		t.Fatalf("SetDefaultAudio: %v", err)
	}

	// Empty id resolves to default.
	got, err := r.AudioClassifier("")
	if err != nil {
		t.Fatalf("default lookup failed: %v", err)
	}
	scores, _ := got.ClassifyAudio(context.Background(), nil, AudioOptions{})
	if scores[0].Label != "hf" {
		t.Errorf("default audio = %q, want hf", scores[0].Label)
	}

	// Explicit id wins over default.
	got, err = r.AudioClassifier("onnx")
	if err != nil {
		t.Fatalf("explicit lookup failed: %v", err)
	}
	scores, _ = got.ClassifyAudio(context.Background(), nil, AudioOptions{})
	if scores[0].Label != "onnx" {
		t.Errorf("explicit id ignored; got %q, want onnx", scores[0].Label)
	}
}

func TestRegistry_SetDefaultRejectsUnregistered(t *testing.T) {
	r := NewRegistry()
	if err := r.SetDefaultAudio("nope"); err == nil {
		t.Error("SetDefaultAudio for unregistered id must error")
	}
	if err := r.SetDefaultText("nope"); err == nil {
		t.Error("SetDefaultText for unregistered id must error")
	}
	if err := r.SetDefaultImage("nope"); err == nil {
		t.Error("SetDefaultImage for unregistered id must error")
	}
	if err := r.SetDefaultVideo("nope"); err == nil {
		t.Error("SetDefaultVideo for unregistered id must error")
	}
	if err := r.SetDefaultEmbedder("nope"); err == nil {
		t.Error("SetDefaultEmbedder for unregistered id must error")
	}
}

func TestRegistry_AllTaskRoundtrips(t *testing.T) {
	r := NewRegistry()
	r.RegisterText("t", &stubText{id: "t"})
	r.RegisterImage("i", &stubImage{id: "i"})
	r.RegisterVideo("v", &stubVideo{id: "v"})
	r.RegisterEmbedder("e", &stubEmbedder{id: "e"})

	if c, err := r.TextClassifier("t"); err != nil || c == nil {
		t.Errorf("text lookup: %v", err)
	}
	if c, err := r.ImageClassifier("i"); err != nil || c == nil {
		t.Errorf("image lookup: %v", err)
	}
	if c, err := r.VideoClassifier("v"); err != nil || c == nil {
		t.Errorf("video lookup: %v", err)
	}
	if e, err := r.Embedder("e"); err != nil || e == nil {
		t.Errorf("embedder lookup: %v", err)
	}
}

func TestRegistry_ContextAttach(t *testing.T) {
	r := NewRegistry()
	r.RegisterAudio("hf", &stubAudio{id: "hf"})

	ctx := WithRegistry(context.Background(), r)
	got := FromContext(ctx)
	if got != r {
		t.Errorf("FromContext returned %v, want %v", got, r)
	}

	// Nil ctx returns nil registry, not panic — defensive for callers
	// that don't have a ctx in hand (e.g. test setup utilities).
	//nolint:staticcheck // SA1012: intentional nil context to exercise defensive branch.
	if got := FromContext(nil); got != nil {
		t.Errorf("FromContext(nil) = %v, want nil", got)
	}

	// Context without a registry returns nil, not error.
	if got := FromContext(context.Background()); got != nil {
		t.Errorf("FromContext(no registry) = %v, want nil", got)
	}
}

func TestRegistry_ErrorMessagesIdentifyMissingID(t *testing.T) {
	r := NewRegistry()
	_, err := r.AudioClassifier("missing-hf")
	if err == nil {
		t.Fatal("expected error")
	}
	// Error must name the looked-up id so misconfiguration is obvious
	// without checking the call site.
	if !strings.Contains(err.Error(), "missing-hf") {
		t.Errorf("error message %q should mention the missing id", err.Error())
	}
}
