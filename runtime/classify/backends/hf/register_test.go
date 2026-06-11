package hf_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	_ "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

func TestHuggingFaceFactoryRegistered(t *testing.T) {
	b, err := classify.CreateFromSpec(classify.ProviderSpec{
		ID:         "hf",
		Type:       "huggingface",
		Credential: credentials.NewAPIKeyCredential("tok"),
	})
	if err != nil {
		t.Fatalf("CreateFromSpec: %v", err)
	}
	if _, ok := b.(classify.AudioClassifier); !ok {
		t.Fatal("HF backend should implement AudioClassifier")
	}
	if _, ok := b.(classify.TextClassifier); !ok {
		t.Fatal("HF backend should implement TextClassifier")
	}
	// HF deliberately does NOT implement VideoClassifier.
	if _, ok := b.(classify.VideoClassifier); ok {
		t.Fatal("HF backend should NOT implement VideoClassifier")
	}
}

func TestHuggingFaceFactory_DedicatedFlag(t *testing.T) {
	_, err := classify.CreateFromSpec(classify.ProviderSpec{
		ID:               "hf",
		Type:             "huggingface",
		BaseURL:          "https://x.endpoints.huggingface.cloud",
		Credential:       credentials.NewAPIKeyCredential("tok"),
		AdditionalConfig: map[string]any{"dedicated": true},
	})
	if err != nil {
		t.Fatalf("CreateFromSpec with dedicated: %v", err)
	}
}
