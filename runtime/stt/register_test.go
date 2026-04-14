package stt_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
)

func TestSTTRegister_OpenAI_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("sk-stub")
	svc, err := stt.CreateFromSpec(stt.ProviderSpec{
		Type:       "openai",
		Model:      "whisper-1",
		BaseURL:    "https://api.openai.test",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestSTTRegister_OpenAI_Defaults(t *testing.T) {
	if _, err := stt.CreateFromSpec(stt.ProviderSpec{Type: "openai"}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestSTTRegister_UnknownType(t *testing.T) {
	if _, err := stt.CreateFromSpec(stt.ProviderSpec{Type: "no-such"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAPIKeyFromCredential_STT(t *testing.T) {
	if got := stt.APIKeyFromCredential(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := stt.APIKeyFromCredential(&credentials.NoOpCredential{}); got != "" {
		t.Errorf("noop = %q", got)
	}
	cred := credentials.NewAPIKeyCredential("k")
	if got := stt.APIKeyFromCredential(cred); got != "k" {
		t.Errorf("apikey = %q", got)
	}
}

func TestResolveCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-env")
	cred, err := stt.ResolveCredential(context.Background(), "openai", "", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := stt.APIKeyFromCredential(cred); got != "sk-env" {
		t.Errorf("got %q want sk-env", got)
	}
}
