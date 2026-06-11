package classify_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// fakeText implements only TextClassifier, to prove partial-interface
// backends register against just the interfaces they satisfy.
type fakeText struct{}

func (fakeText) ClassifyText(_ context.Context, _ string, _ classify.TextOptions) ([]classify.LabelScore, error) {
	return []classify.LabelScore{{Label: "ok", Score: 1}}, nil
}

// fakeAll implements every task interface so RegisterBackend covers all paths.
type fakeAll struct{}

func (fakeAll) ClassifyText(_ context.Context, _ string, _ classify.TextOptions) ([]classify.LabelScore, error) {
	return nil, nil
}
func (fakeAll) ClassifyAudio(_ context.Context, _ []byte, _ classify.AudioOptions) ([]classify.LabelScore, error) {
	return nil, nil
}
func (fakeAll) ClassifyImage(_ context.Context, _ []byte, _ classify.ImageOptions) ([]classify.LabelScore, error) {
	return nil, nil
}
func (fakeAll) ClassifyVideo(_ context.Context, _ []byte, _ classify.VideoOptions) ([]classify.LabelScore, error) {
	return nil, nil
}
func (fakeAll) Embed(_ context.Context, _ []string, _ classify.EmbedOptions) ([][]float32, error) {
	return nil, nil
}

func TestRegisterBackend_PartialInterface(t *testing.T) {
	reg := classify.NewRegistry()
	tasks := classify.RegisterBackend(reg, "fake", fakeText{})
	if len(tasks) != 1 || tasks[0] != "text" {
		t.Fatalf("expected [text], got %v", tasks)
	}
	if _, err := reg.TextClassifier("fake"); err != nil {
		t.Fatalf("text classifier should resolve: %v", err)
	}
	if _, err := reg.AudioClassifier("fake"); err == nil {
		t.Fatal("audio classifier should NOT be registered for a text-only backend")
	}
}

func TestBuildRegistry_FirstWinsDefault(t *testing.T) {
	classify.RegisterFactory("faketext", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeText{}, nil
	})
	reg, err := classify.BuildRegistry(
		[]classify.ProviderSpec{{ID: "a", Type: "faketext"}, {ID: "b", Type: "faketext"}},
		classify.RegistryDefaults{},
	)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	// Empty id resolves to the first-declared provider that implements the task.
	c, err := reg.TextClassifier("")
	if err != nil {
		t.Fatalf("default text classifier: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil default text classifier")
	}
}

func TestBuildRegistry_EmptyReturnsNil(t *testing.T) {
	reg, err := classify.BuildRegistry(nil, classify.RegistryDefaults{})
	if err != nil || reg != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", reg, err)
	}
}

func TestBuildRegistry_ExplicitDefaultOverrides(t *testing.T) {
	classify.RegisterFactory("faketext2", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeText{}, nil
	})
	reg, err := classify.BuildRegistry(
		[]classify.ProviderSpec{{ID: "a", Type: "faketext2"}, {ID: "b", Type: "faketext2"}},
		classify.RegistryDefaults{TextClassifier: "b"},
	)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, err := reg.TextClassifier(""); err != nil {
		t.Fatalf("explicit default should resolve: %v", err)
	}
}

func TestBuildRegistry_UnknownType(t *testing.T) {
	_, err := classify.BuildRegistry(
		[]classify.ProviderSpec{{ID: "x", Type: "nope"}},
		classify.RegistryDefaults{},
	)
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
}

func TestBuildRegistry_EmptyIDError(t *testing.T) {
	classify.RegisterFactory("fakeid", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeText{}, nil
	})
	_, err := classify.BuildRegistry(
		[]classify.ProviderSpec{{Type: "fakeid"}}, // no ID
		classify.RegistryDefaults{},
	)
	if err == nil {
		t.Fatal("expected error for empty provider id")
	}
}

func TestRegisterBackend_AllInterfaces(t *testing.T) {
	reg := classify.NewRegistry()
	tasks := classify.RegisterBackend(reg, "all", fakeAll{})
	// Should register all 5 task types.
	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks registered, got %d: %v", len(tasks), tasks)
	}
	if _, err := reg.AudioClassifier("all"); err != nil {
		t.Fatalf("audio: %v", err)
	}
	if _, err := reg.TextClassifier("all"); err != nil {
		t.Fatalf("text: %v", err)
	}
	if _, err := reg.ImageClassifier("all"); err != nil {
		t.Fatalf("image: %v", err)
	}
	if _, err := reg.VideoClassifier("all"); err != nil {
		t.Fatalf("video: %v", err)
	}
	if _, err := reg.Embedder("all"); err != nil {
		t.Fatalf("embedder: %v", err)
	}
}

func TestRegisterBackend_NoInterface(t *testing.T) {
	reg := classify.NewRegistry()
	tasks := classify.RegisterBackend(reg, "none", struct{}{})
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %v", tasks)
	}
}

func TestBuildRegistry_AllTaskDefaults(t *testing.T) {
	classify.RegisterFactory("fakeall", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeAll{}, nil
	})
	reg, err := classify.BuildRegistry(
		[]classify.ProviderSpec{{ID: "x", Type: "fakeall"}},
		classify.RegistryDefaults{},
	)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	// All task defaults should be set to "x" by first-wins.
	if _, err := reg.AudioClassifier(""); err != nil {
		t.Fatalf("default audio: %v", err)
	}
	if _, err := reg.TextClassifier(""); err != nil {
		t.Fatalf("default text: %v", err)
	}
	if _, err := reg.ImageClassifier(""); err != nil {
		t.Fatalf("default image: %v", err)
	}
	if _, err := reg.VideoClassifier(""); err != nil {
		t.Fatalf("default video: %v", err)
	}
	if _, err := reg.Embedder(""); err != nil {
		t.Fatalf("default embedder: %v", err)
	}
}

func TestResolveCredential_APIKey(t *testing.T) {
	cred, err := classify.ResolveCredential(
		context.Background(), "huggingface", "",
		&credentials.CredentialConfig{APIKey: "test-key"},
	)
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("expected non-nil credential")
	}
}
