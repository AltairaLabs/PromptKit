package voyageai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestVoyageRerank_SendsTheDocumentedWireFormat(t *testing.T) {
	var got voyageRerankRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("path = %q, want /rerank", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":[{"index":1,"relevance_score":0.9,"document":"b"}],
			"model":"rerank-2.5","usage":{"total_tokens":42}}`))
	}))
	defer srv.Close()

	p, err := NewRerankProvider(WithRerankAPIKey("test-key"), WithRerankBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "which one", Documents: []string{"a", "b"}, TopN: 1,
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	if got.Model != DefaultRerankModel {
		t.Errorf("model = %q, want the default", got.Model)
	}
	if got.Query != "which one" || len(got.Documents) != 2 {
		t.Errorf("query/documents not sent: %+v", got)
	}
	if got.TopK != 1 {
		t.Errorf("top_k = %d, want 1 — Voyage spells TopN as top_k", got.TopK)
	}
	if !got.ReturnDocuments {
		t.Error("return_documents should be requested so callers get the text back")
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Index != 1 {
		t.Errorf("index = %d, want the index Voyage reported", resp.Results[0].Index)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 42 {
		t.Errorf("usage not carried through: %+v", resp.Usage)
	}
}

// TestVoyageRerank_RejectsAnOutOfRangeIndex covers the guard that matters most
// in this file. An index outside the request would make the caller read a
// different document than the one that was scored — a silent mix-up, not a
// crash, so it has to be refused rather than passed on.
func TestVoyageRerank_RejectsAnOutOfRangeIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":7,"relevance_score":0.9}],"model":"m"}`))
	}))
	defer srv.Close()

	p, _ := NewRerankProvider(WithRerankAPIKey("k"), WithRerankBaseURL(srv.URL))
	_, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "q", Documents: []string{"a", "b"},
	})
	if err == nil || !strings.Contains(err.Error(), "outside the 2 documents sent") {
		t.Fatalf("expected an out-of-range rejection, got %v", err)
	}
}

func TestVoyageRerank_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"slow down"}`))
	}))
	defer srv.Close()

	p, _ := NewRerankProvider(WithRerankAPIKey("k"), WithRerankBaseURL(srv.URL))
	_, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "q", Documents: []string{"a"},
	})
	if err == nil {
		t.Fatal("expected the 429 to surface")
	}
}

func TestNewRerankProvider_RequiresAKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	if _, err := NewRerankProvider(); err == nil {
		t.Fatal("expected a missing-key error")
	}
}

// TestVoyageRerank_RegisteredWithTheFactory is the test that catches the
// inert-declaration failure: the provider compiles, the constructor works, and
// nothing can reach it because the init() never ran or the package is never
// imported. Only the factory path proves the wiring.
func TestVoyageRerank_RegisteredWithTheFactory(t *testing.T) {
	rp, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		ID: "vr", Type: "voyageai", Credential: credentials.NewAPIKeyCredential("k"),
	})
	if err != nil {
		t.Fatalf("voyageai must be resolvable through the rerank factory: %v", err)
	}
	if rp.ID() != "voyageai-rerank" {
		t.Errorf("ID = %q", rp.ID())
	}
	if rp.MaxDocuments() != voyageRerankMaxDocs {
		t.Errorf("MaxDocuments = %d, want %d", rp.MaxDocuments(), voyageRerankMaxDocs)
	}
}

// TestVoyageRerankOptions_AllApply covers the setters. Each is a one-liner, but
// an option that silently does nothing is the kind of thing that only shows up
// as "why is it still using the default model" against a live API.
func TestVoyageRerankOptions_AllApply(t *testing.T) {
	client := &http.Client{}
	p, err := NewRerankProvider(
		WithRerankAPIKey("k"),
		WithRerankModel(ModelRerank25Lite),
		WithRerankBaseURL("https://example.test"),
		WithRerankHTTPClient(client),
		WithRerankTruncation(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model() != ModelRerank25Lite {
		t.Errorf("model = %q", p.Model())
	}
	if p.BaseURL != "https://example.test" {
		t.Errorf("baseURL = %q", p.BaseURL)
	}
	if p.HTTPClient != client {
		t.Error("HTTP client not applied")
	}
	if p.truncation == nil || !*p.truncation {
		t.Error("truncation not applied")
	}
}

// TestVoyageRerank_FactoryAppliesSpecFields proves the register's optional
// branches are wired, not just present — a factory that ignores spec.Model
// would quietly run every request on the default.
func TestVoyageRerank_FactoryAppliesSpecFields(t *testing.T) {
	rp, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		ID: "vr", Type: "voyageai", Model: ModelRerank25Lite,
		BaseURL:          "https://example.test",
		Credential:       credentials.NewAPIKeyCredential("k"),
		AdditionalConfig: map[string]any{"truncation": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := rp.(*RerankProvider)
	if !ok {
		t.Fatalf("factory returned %T", rp)
	}
	if p.Model() != ModelRerank25Lite {
		t.Errorf("spec.Model ignored: %q", p.Model())
	}
	if p.BaseURL != "https://example.test" {
		t.Errorf("spec.BaseURL ignored: %q", p.BaseURL)
	}
	if p.truncation == nil || !*p.truncation {
		t.Error("additional_config truncation ignored")
	}
}

// TestVoyageRerank_FactoryRejectsPlatform confirms the register surfaces the
// role's platform rejection rather than building a provider that cannot auth.
func TestVoyageRerank_FactoryRejectsPlatform(t *testing.T) {
	_, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		ID: "vr", Type: "voyageai", Platform: "bedrock",
		Credential: credentials.NewAPIKeyCredential("k"),
	})
	if err == nil || !strings.Contains(err.Error(), "not supported for the rerank role") {
		t.Fatalf("expected a platform rejection, got %v", err)
	}
}
