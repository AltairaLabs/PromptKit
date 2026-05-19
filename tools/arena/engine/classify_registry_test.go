package engine

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestBuildClassifyRegistry_NoInferenceReturnsNil(t *testing.T) {
	cfg := &config.Config{}
	reg, err := buildClassifyRegistry(cfg)
	if err != nil {
		t.Fatalf("buildClassifyRegistry: %v", err)
	}
	if reg != nil {
		t.Errorf("expected nil registry when no inference entries; got %v", reg)
	}
}

func TestBuildClassifyRegistry_NilConfigReturnsNil(t *testing.T) {
	reg, err := buildClassifyRegistry(nil)
	if err != nil {
		t.Fatalf("buildClassifyRegistry(nil): %v", err)
	}
	if reg != nil {
		t.Error("expected nil registry for nil config")
	}
}

func TestBuildClassifyRegistry_HFEntryRegistersAllTasks(t *testing.T) {
	t.Setenv("HF_TOKEN", "test-token")
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"hf": {ID: "hf", Type: "huggingface"},
		},
	}
	reg, err := buildClassifyRegistry(cfg)
	if err != nil {
		t.Fatalf("buildClassifyRegistry: %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	// HF Client implements every task interface; the registry should
	// resolve the id for each. Video deliberately omitted (no HF endpoint).
	for _, lookup := range []struct {
		name string
		fn   func() error
	}{
		{"audio", func() error { _, err := reg.AudioClassifier("hf"); return err }},
		{"text", func() error { _, err := reg.TextClassifier("hf"); return err }},
		{"image", func() error { _, err := reg.ImageClassifier("hf"); return err }},
		{"embedder", func() error { _, err := reg.Embedder("hf"); return err }},
	} {
		if err := lookup.fn(); err != nil {
			t.Errorf("%s lookup: %v", lookup.name, err)
		}
	}
	// Video classifier should NOT be registered — HF has no general endpoint.
	if _, err := reg.VideoClassifier("hf"); err == nil {
		t.Error("video should not be registered for HF backend")
	}
}

func TestBuildClassifyRegistry_LiteralAPIKeyWinsOverEnv(t *testing.T) {
	t.Setenv("HF_TOKEN", "env-token")
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"hf": {ID: "hf", Type: "huggingface", APIKey: "literal-token"},
		},
	}
	if _, err := buildClassifyRegistry(cfg); err != nil {
		t.Errorf("literal api key should succeed without HF_TOKEN: %v", err)
	}
}

func TestBuildClassifyRegistry_MissingAPIKeyErrors(t *testing.T) {
	// Clear inherited values so the canonical fallback can't accidentally
	// supply a token in CI environments where HF_TOKEN is exported.
	t.Setenv("HF_TOKEN", "")
	t.Setenv("HUGGING_FACE_HUB_TOKEN", "")
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"hf": {ID: "hf", Type: "huggingface"},
		},
	}
	_, err := buildClassifyRegistry(cfg)
	if err == nil {
		t.Fatal("expected error when no api key resolvable")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Errorf("error %q should mention api_key", err.Error())
	}
}

func TestBuildClassifyRegistry_NamedEnvVarMustBeSet(t *testing.T) {
	t.Setenv("HF_TOKEN", "fallback-should-not-be-used")
	t.Setenv("MY_HF_KEY", "") // explicitly empty
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"hf": {ID: "hf", Type: "huggingface", APIKeyEnv: "MY_HF_KEY"},
		},
	}
	_, err := buildClassifyRegistry(cfg)
	if err == nil {
		t.Fatal("when api_key_env is set, the named var being empty should error (don't silently fall through to HF_TOKEN)")
	}
	if !strings.Contains(err.Error(), "MY_HF_KEY") {
		t.Errorf("error %q should name the configured env var", err.Error())
	}
}

func TestBuildClassifyRegistry_UnsupportedTypeErrors(t *testing.T) {
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"x": {ID: "x", Type: "bogus"},
		},
	}
	_, err := buildClassifyRegistry(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q should name the offending type", err.Error())
	}
}

func TestBuildClassifyRegistry_DefaultsAppliedToRegistry(t *testing.T) {
	t.Setenv("HF_TOKEN", "test-token")
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"hf": {ID: "hf", Type: "huggingface"},
		},
		Defaults: config.Defaults{
			Inference: &config.InferenceDefaults{
				AudioClassifier: "hf",
				TextClassifier:  "hf",
				ImageClassifier: "hf",
				Embedder:        "hf",
			},
		},
	}
	reg, err := buildClassifyRegistry(cfg)
	if err != nil {
		t.Fatalf("buildClassifyRegistry: %v", err)
	}
	// Empty-id lookup should now resolve via the configured default.
	if _, err := reg.AudioClassifier(""); err != nil {
		t.Errorf("audio default not applied: %v", err)
	}
	if _, err := reg.TextClassifier(""); err != nil {
		t.Errorf("text default not applied: %v", err)
	}
}

func TestBuildClassifyRegistry_DefaultsReferencingUnregisteredID(t *testing.T) {
	t.Setenv("HF_TOKEN", "test-token")
	cfg := &config.Config{
		LoadedInference: map[string]*config.InferenceConfig{
			"hf": {ID: "hf", Type: "huggingface"},
		},
		Defaults: config.Defaults{
			Inference: &config.InferenceDefaults{
				AudioClassifier: "doesnotexist",
			},
		},
	}
	_, err := buildClassifyRegistry(cfg)
	if err == nil {
		t.Fatal("expected error when default references an unknown id")
	}
	if !strings.Contains(err.Error(), "audio_classifier") {
		t.Errorf("error %q should identify which default failed", err.Error())
	}
}
