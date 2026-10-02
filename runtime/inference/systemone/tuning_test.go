package systemone

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

func TestCreateFromSpec_TuningRequestTimeout(t *testing.T) {
	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		Type:       providerType,
		BaseURL:    "https://api.typesafe.ai/v1",
		Credential: credentials.NewAPIKeyCredential("tok"),
		Tuning:     base.HTTPTuning{RequestTimeout: 3 * time.Second},
	})
	if err != nil {
		t.Fatalf("CreateFromSpec: %v", err)
	}
	p, ok := got.(*Provider)
	if !ok {
		t.Fatalf("got %T, want *Provider", got)
	}
	if p.http.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", p.http.Timeout)
	}
}
