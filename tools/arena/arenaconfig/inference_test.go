package arenaconfig

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// freshInferenceConfig returns a Config with LoadedInferenceProviders
// pre-initialized, matching the state LoadConfig hands to
// validateInferenceDefaults.
func freshInferenceConfig() *Config {
	return &Config{
		LoadedInferenceProviders: make(map[string]*config.Provider),
	}
}

func TestRoleInference_AcceptedByValidator(t *testing.T) {
	p := &config.Provider{Role: config.RoleInference}
	if err := p.ValidateRole(); err != nil {
		t.Fatalf("role: inference must validate cleanly: %v", err)
	}
	if p.GetRole() != config.RoleInference {
		t.Errorf("GetRole = %q, want %q", p.GetRole(), config.RoleInference)
	}
}

func TestValidateInferenceDefaults_NilSectionAccepted(t *testing.T) {
	c := freshInferenceConfig()
	// No Defaults.Inference set — must not error.
	if err := c.validateInferenceDefaults(); err != nil {
		t.Fatalf("validateInferenceDefaults: %v", err)
	}
}

func TestValidateInferenceDefaults_EmptyIDsAccepted(t *testing.T) {
	c := freshInferenceConfig()
	c.Defaults.Inference = &config.InferenceDefaults{} // all task fields empty
	if err := c.validateInferenceDefaults(); err != nil {
		t.Fatalf("zero-value InferenceDefaults must validate: %v", err)
	}
}

func TestValidateInferenceDefaults_KnownIDsAccepted(t *testing.T) {
	c := freshInferenceConfig()
	c.LoadedInferenceProviders["hf"] = &config.Provider{ID: "hf", Type: "huggingface", Role: config.RoleInference}
	c.Defaults.Inference = &config.InferenceDefaults{
		AudioClassifier: "hf",
		TextClassifier:  "hf",
		ImageClassifier: "hf",
		VideoClassifier: "hf",
		Embedder:        "hf",
	}
	if err := c.validateInferenceDefaults(); err != nil {
		t.Fatalf("validateInferenceDefaults: %v", err)
	}
}

func TestValidateInferenceDefaults_UnknownIDRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(d *config.InferenceDefaults)
		errSubs string
	}{
		{"audio", func(d *config.InferenceDefaults) { d.AudioClassifier = "nope" }, "audio_classifier"},
		{"text", func(d *config.InferenceDefaults) { d.TextClassifier = "nope" }, "text_classifier"},
		{"image", func(d *config.InferenceDefaults) { d.ImageClassifier = "nope" }, "image_classifier"},
		{"video", func(d *config.InferenceDefaults) { d.VideoClassifier = "nope" }, "video_classifier"},
		{"embedder", func(d *config.InferenceDefaults) { d.Embedder = "nope" }, "embedder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := freshInferenceConfig()
			c.LoadedInferenceProviders["hf"] = &config.Provider{ID: "hf", Type: "huggingface", Role: config.RoleInference}
			d := &config.InferenceDefaults{}
			tc.mutate(d)
			c.Defaults.Inference = d
			err := c.validateInferenceDefaults()
			if err == nil {
				t.Fatalf("expected error for unknown %s id", tc.name)
			}
			if !strings.Contains(err.Error(), tc.errSubs) {
				t.Errorf("error %q should mention %q", err.Error(), tc.errSubs)
			}
			if !strings.Contains(err.Error(), "nope") {
				t.Errorf("error %q should name the offending id", err.Error())
			}
			if !strings.Contains(err.Error(), "role: inference") {
				t.Errorf("error %q should mention role: inference so users know how to fix", err.Error())
			}
		})
	}
}
