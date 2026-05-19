package config

import (
	"strings"
	"testing"
)

// freshConfig returns a Config with LoadedInference initialized, matching
// the state LoadConfig hands to loadInference. The other Loaded* maps are
// not needed by loadInference.
func freshConfig() *Config {
	return &Config{
		LoadedInference: make(map[string]*InferenceConfig),
	}
}

func TestLoadInference_EmptyList(t *testing.T) {
	c := freshConfig()
	if err := c.loadInference(); err != nil {
		t.Fatalf("loadInference: %v", err)
	}
	if len(c.LoadedInference) != 0 {
		t.Errorf("LoadedInference = %d entries, want 0", len(c.LoadedInference))
	}
}

func TestLoadInference_SingleEntryPopulatesMap(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{
		{ID: "hf", Type: "huggingface", APIKeyEnv: "HF_TOKEN"},
	}
	if err := c.loadInference(); err != nil {
		t.Fatalf("loadInference: %v", err)
	}
	got, ok := c.LoadedInference["hf"]
	if !ok {
		t.Fatal("LoadedInference[hf] missing")
	}
	if got.Type != "huggingface" {
		t.Errorf("Type = %q, want huggingface", got.Type)
	}
	if got.APIKeyEnv != "HF_TOKEN" {
		t.Errorf("APIKeyEnv = %q, want HF_TOKEN", got.APIKeyEnv)
	}
}

func TestLoadInference_MultipleEntries(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{
		{ID: "hf", Type: "huggingface"},
		{ID: "hf-dedicated", Type: "huggingface", BaseURL: "https://my-endpoint.huggingface.cloud", Dedicated: true},
	}
	if err := c.loadInference(); err != nil {
		t.Fatalf("loadInference: %v", err)
	}
	if len(c.LoadedInference) != 2 {
		t.Fatalf("LoadedInference = %d entries, want 2", len(c.LoadedInference))
	}
	if !c.LoadedInference["hf-dedicated"].Dedicated {
		t.Error("Dedicated flag not preserved on the dedicated entry")
	}
}

func TestLoadInference_MissingIDRejected(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{{Type: "huggingface"}}
	err := c.loadInference()
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("error %q should mention missing id", err.Error())
	}
}

func TestLoadInference_MissingTypeRejected(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{{ID: "hf"}}
	err := c.loadInference()
	if err == nil {
		t.Fatal("expected error for empty type")
	}
	if !strings.Contains(err.Error(), "type is required") {
		t.Errorf("error %q should mention missing type", err.Error())
	}
}

func TestLoadInference_DuplicateIDRejected(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{
		{ID: "hf", Type: "huggingface"},
		{ID: "hf", Type: "huggingface"},
	}
	err := c.loadInference()
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q should mention duplicate", err.Error())
	}
}

func TestLoadInference_DefaultsAcceptKnownIDs(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{{ID: "hf", Type: "huggingface"}}
	c.Defaults.Inference = &InferenceDefaults{
		AudioClassifier: "hf",
		TextClassifier:  "hf",
		ImageClassifier: "hf",
		VideoClassifier: "hf",
		Embedder:        "hf",
	}
	if err := c.loadInference(); err != nil {
		t.Fatalf("loadInference: %v", err)
	}
}

func TestLoadInference_DefaultsRejectUnknownID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(d *InferenceDefaults)
		errSubs string
	}{
		{"audio", func(d *InferenceDefaults) { d.AudioClassifier = "nope" }, "audio_classifier"},
		{"text", func(d *InferenceDefaults) { d.TextClassifier = "nope" }, "text_classifier"},
		{"image", func(d *InferenceDefaults) { d.ImageClassifier = "nope" }, "image_classifier"},
		{"video", func(d *InferenceDefaults) { d.VideoClassifier = "nope" }, "video_classifier"},
		{"embedder", func(d *InferenceDefaults) { d.Embedder = "nope" }, "embedder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := freshConfig()
			c.Inference = []InferenceConfig{{ID: "hf", Type: "huggingface"}}
			d := &InferenceDefaults{}
			tc.mutate(d)
			c.Defaults.Inference = d
			err := c.loadInference()
			if err == nil {
				t.Fatalf("expected error for unknown %s id", tc.name)
			}
			if !strings.Contains(err.Error(), tc.errSubs) {
				t.Errorf("error %q should mention %q", err.Error(), tc.errSubs)
			}
			if !strings.Contains(err.Error(), "nope") {
				t.Errorf("error %q should name the offending id", err.Error())
			}
		})
	}
}

func TestLoadInference_EmptyDefaultsAreAccepted(t *testing.T) {
	c := freshConfig()
	c.Inference = []InferenceConfig{{ID: "hf", Type: "huggingface"}}
	c.Defaults.Inference = &InferenceDefaults{} // all fields empty
	if err := c.loadInference(); err != nil {
		t.Fatalf("loadInference rejected zero-value defaults: %v", err)
	}
}
