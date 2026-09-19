package cohere

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

func TestCohereRerank_SendsTheDocumentedWireFormat(t *testing.T) {
	var got cohereRerankRequest
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
		_, _ = w.Write([]byte(`{"id":"x","results":[
			{"index":2,"relevance_score":0.88},{"index":0,"relevance_score":0.41}],
			"meta":{"billed_units":{"search_units":1}}}`))
	}))
	defer srv.Close()

	p, err := NewRerankProvider(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "which one", Documents: []string{"a", "b", "c"}, TopN: 2,
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	if got.Model != DefaultRerankModel {
		t.Errorf("model = %q, want the default", got.Model)
	}
	if got.TopN != 2 {
		t.Errorf("top_n = %d, want 2", got.TopN)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].Index != 2 || resp.Results[1].Index != 0 {
		t.Errorf("indices not preserved in Cohere's order: %+v", resp.Results)
	}
}

// TestCohereRerank_FillsDocumentTextFromTheRequest covers the one place Cohere
// differs from Voyage in a way callers would notice: v2 returns indices and
// scores only, no document text. Leaving Document empty would make the same
// code behave differently depending on which backend was configured.
func TestCohereRerank_FillsDocumentTextFromTheRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9}]}`))
	}))
	defer srv.Close()

	p, _ := NewRerankProvider(WithAPIKey("k"), WithBaseURL(srv.URL))
	resp, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "q", Documents: []string{"first", "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Results[0].Document != "second" {
		t.Errorf("Document = %q, want the text at the returned index", resp.Results[0].Document)
	}
}

// TestCohereRerank_ReportsNoTokenUsage pins a deliberate omission: Cohere bills
// rerank in search units, not tokens. Putting search units in TotalTokens
// would be a number a cost dashboard would happily add to real token counts.
func TestCohereRerank_ReportsNoTokenUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"index":0,"relevance_score":0.5}],
			"meta":{"billed_units":{"search_units":3}}}`))
	}))
	defer srv.Close()

	p, _ := NewRerankProvider(WithAPIKey("k"), WithBaseURL(srv.URL))
	resp, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "q", Documents: []string{"a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage != nil {
		t.Errorf("Usage should be nil — search units are not tokens, got %+v", resp.Usage)
	}
}

func TestCohereRerank_RejectsAnOutOfRangeIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"index":9,"relevance_score":0.9}]}`))
	}))
	defer srv.Close()

	p, _ := NewRerankProvider(WithAPIKey("k"), WithBaseURL(srv.URL))
	_, err := p.Rerank(context.Background(), providers.RerankRequest{
		Query: "q", Documents: []string{"a"},
	})
	if err == nil || !strings.Contains(err.Error(), "outside the 1 documents sent") {
		t.Fatalf("expected an out-of-range rejection, got %v", err)
	}
}

func TestNewRerankProvider_RequiresAKey(t *testing.T) {
	t.Setenv("COHERE_API_KEY", "")
	if _, err := NewRerankProvider(); err == nil {
		t.Fatal("expected a missing-key error")
	}
}

// TestCohereRerank_RegisteredWithTheFactory proves the init() wiring, which a
// constructor test cannot.
func TestCohereRerank_RegisteredWithTheFactory(t *testing.T) {
	rp, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		ID: "cr", Type: "cohere", Credential: credentials.NewAPIKeyCredential("k"),
	})
	if err != nil {
		t.Fatalf("cohere must be resolvable through the rerank factory: %v", err)
	}
	if rp.ID() != "cohere-rerank" {
		t.Errorf("ID = %q", rp.ID())
	}
}

// TestCohereRerankOptions_AllApply covers the setters.
func TestCohereRerankOptions_AllApply(t *testing.T) {
	client := &http.Client{}
	p, err := NewRerankProvider(
		WithAPIKey("k"),
		WithModel(ModelRerankEnglishV30),
		WithBaseURL("https://example.test"),
		WithHTTPClient(client),
		WithMaxTokensPerDoc(512),
	)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model() != ModelRerankEnglishV30 {
		t.Errorf("model = %q", p.Model())
	}
	if p.BaseURL != "https://example.test" {
		t.Errorf("baseURL = %q", p.BaseURL)
	}
	if p.HTTPClient != client {
		t.Error("HTTP client not applied")
	}
	if p.maxTokensPerDoc != 512 {
		t.Errorf("maxTokensPerDoc = %d", p.maxTokensPerDoc)
	}
}

// TestCohereRerank_FactoryAppliesSpecFields proves the register's optional
// branches are wired rather than merely present.
func TestCohereRerank_FactoryAppliesSpecFields(t *testing.T) {
	rp, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		ID: "cr", Type: "cohere", Model: ModelRerankEnglishV30,
		BaseURL:          "https://example.test",
		Credential:       credentials.NewAPIKeyCredential("k"),
		AdditionalConfig: map[string]any{"max_tokens_per_doc": 256},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := rp.(*RerankProvider)
	if !ok {
		t.Fatalf("factory returned %T", rp)
	}
	if p.Model() != ModelRerankEnglishV30 {
		t.Errorf("spec.Model ignored: %q", p.Model())
	}
	if p.BaseURL != "https://example.test" {
		t.Errorf("spec.BaseURL ignored: %q", p.BaseURL)
	}
	if p.maxTokensPerDoc != 256 {
		t.Errorf("additional_config max_tokens_per_doc ignored: %d", p.maxTokensPerDoc)
	}
}

func TestCohereRerank_FactoryRejectsPlatform(t *testing.T) {
	_, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		ID: "cr", Type: "cohere", Platform: "azure",
		Credential: credentials.NewAPIKeyCredential("k"),
	})
	if err == nil || !strings.Contains(err.Error(), "not supported for the rerank role") {
		t.Fatalf("expected a platform rejection, got %v", err)
	}
}
