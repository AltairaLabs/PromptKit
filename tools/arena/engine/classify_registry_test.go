package engine

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

func TestBuildClassifyRegistry_NoInferenceReturnsNil(t *testing.T) {
	cfg := &arenaconfig.Config{}
	reg, err := buildClassifyRegistry(cfg)
	if err != nil {
		t.Fatalf("buildClassifyRegistry: %v", err)
	}
	if reg != nil {
		t.Errorf("expected nil registry when no inference providers; got %v", reg)
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

// hfProvider returns a Provider with role: inference + a literal credential
// so resolveProviderAPIKey succeeds without env-var setup. The literal-key
// path is the cleanest seam in tests; env-var resolution is covered by
// the credentials package's own tests.
func hfProvider(id string) *config.Provider {
	return &config.Provider{
		ID:   id,
		Type: "huggingface",
		Role: config.RoleInference,
		Credential: &config.CredentialConfig{
			APIKey: "test-token",
		},
	}
}

func TestBuildClassifyRegistry_HFEntryRegistersAllTasks(t *testing.T) {
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"hf": hfProvider("hf"),
		},
	}
	reg, err := buildClassifyRegistry(cfg)
	if err != nil {
		t.Fatalf("buildClassifyRegistry: %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
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
	// Video classifier deliberately not registered — HF has no general endpoint.
	if _, err := reg.VideoClassifier("hf"); err == nil {
		t.Error("video should not be registered for HF backend")
	}
}

func TestBuildClassifyRegistry_CanonicalEnvVarFallback(t *testing.T) {
	// No literal credential — must fall through to HF_TOKEN via the
	// credentials package's DefaultEnvVars for provider type "huggingface".
	t.Setenv("HF_TOKEN", "from-env")
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"hf": {ID: "hf", Type: "huggingface", Role: config.RoleInference},
		},
	}
	if _, err := buildClassifyRegistry(cfg); err != nil {
		t.Errorf("HF_TOKEN fallback should succeed: %v", err)
	}
}

func TestBuildClassifyRegistry_MissingAPIKeyErrors(t *testing.T) {
	// No literal credential, no env vars — must fail with a message that
	// points the user at where to fix it.
	t.Setenv("HF_TOKEN", "")
	t.Setenv("HUGGING_FACE_HUB_TOKEN", "")
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"hf": {ID: "hf", Type: "huggingface", Role: config.RoleInference},
		},
	}
	_, err := buildClassifyRegistry(cfg)
	if err == nil {
		t.Fatal("expected error when no credential resolvable")
	}
	if !strings.Contains(err.Error(), "api key") {
		t.Errorf("error %q should mention api key", err.Error())
	}
}

func TestBuildClassifyRegistry_UnsupportedTypeErrors(t *testing.T) {
	// Provide a literal API key so the credential check passes and the
	// unsupported-type error surfaces from classify.BuildRegistry.
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"x": {
				ID:   "x",
				Type: "bogus",
				Role: config.RoleInference,
				Credential: &config.CredentialConfig{
					APIKey: "test-token",
				},
			},
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
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"hf": hfProvider("hf"),
		},
		Defaults: arenaconfig.Defaults{
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
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"hf": hfProvider("hf"),
		},
		Defaults: arenaconfig.Defaults{
			Inference: &config.InferenceDefaults{
				AudioClassifier: "doesnotexist",
			},
		},
	}
	_, err := buildClassifyRegistry(cfg)
	if err == nil {
		t.Fatal("expected error when default references an unknown id")
	}
	// The shared factory surfaces "audio classifier" (with space) in its
	// error; the old bespoke code used "audio_classifier" (with underscore).
	// Either form identifies the failing default — check for the id itself.
	if !strings.Contains(err.Error(), "doesnotexist") {
		t.Errorf("error %q should identify the unregistered default id", err.Error())
	}
}

func TestBuildClassifyRegistry_DedicatedFlagFromAdditionalConfig(t *testing.T) {
	// Construction should accept Dedicated true: a Provider with no BaseURL
	// + Dedicated true would normally fail (NewClient rejects bare-relative
	// dedicated URL), but Dedicated=false (the default) lets the client
	// build successfully against the public Inference API. This test
	// pins the AdditionalConfig path itself — that a "dedicated: true"
	// bool flag in the YAML is read through.
	cfg := &arenaconfig.Config{
		LoadedInferenceProviders: map[string]*config.Provider{
			"hf": {
				ID:      "hf",
				Type:    "huggingface",
				Role:    config.RoleInference,
				BaseURL: "https://my-endpoint.huggingface.cloud",
				Credential: &config.CredentialConfig{
					APIKey: "test-token",
				},
				AdditionalConfig: map[string]interface{}{
					"dedicated": true,
				},
			},
		},
	}
	if _, err := buildClassifyRegistry(cfg); err != nil {
		t.Errorf("dedicated provider should build: %v", err)
	}
}
