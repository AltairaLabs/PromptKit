package mock

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestCreateProviderFromSpec_AppliesMessageMetadataTemplate(t *testing.T) {
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "m", Type: "mock", MessageMetadataTemplate: "<context>{{selection}}</context>",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	mt, ok := p.(providers.MessageMetadataTemplater)
	if !ok {
		t.Fatalf("%T does not implement MessageMetadataTemplater", p)
	}
	if got := mt.MessageMetadataTemplate(); got != "<context>{{selection}}</context>" {
		t.Fatalf("template = %q", got)
	}
}
