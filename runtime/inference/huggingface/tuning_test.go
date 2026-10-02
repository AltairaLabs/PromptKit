package huggingface

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestCreateFromSpec_TuningHeaderReachesServer(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Gateway")
		fmt.Fprintln(w, `[{"label":"angry","score":0.9}]`)
	}))
	defer srv.Close()

	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		Type: providerType, BaseURL: srv.URL,
		Credential: credentials.NewAPIKeyCredential("tok"),
		Tuning:     base.HTTPTuning{Headers: map[string]string{"X-Gateway": "gw"}},
	})
	if err != nil {
		t.Fatalf("CreateFromSpec: %v", err)
	}
	_, err = got.Infer(context.Background(), inference.Request{
		Model:  "superb/wav2vec2-base-superb-er",
		Inputs: []types.Message{audioPart([]byte("RIFF...WAV"), "audio/wav")},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if gotHeader != "gw" {
		t.Errorf("X-Gateway = %q, want %q", gotHeader, "gw")
	}
}
