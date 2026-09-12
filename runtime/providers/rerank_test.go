package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

func TestMockRerankProvider_OrdersByTermOverlap(t *testing.T) {
	p := NewMockRerankProvider()
	got, err := p.Rerank(context.Background(), RerankRequest{
		Query: "refund policy",
		Documents: []string{
			"our shipping times are two days",     // 0 terms
			"the refund policy allows 30 days",    // 2 terms
			"refunds are processed within a week", // 1 term ("refund" is a substring of "refunds")
		},
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(got.Results) != 3 {
		t.Fatalf("expected all 3 documents back, got %d", len(got.Results))
	}

	// Index, not position, identifies the document — that is the whole point
	// of the field, so assert on it rather than on Document.
	wantOrder := []int{1, 2, 0}
	for i, want := range wantOrder {
		if got.Results[i].Index != want {
			t.Errorf("position %d: index = %d, want %d (order was %v)",
				i, got.Results[i].Index, want, indices(got.Results))
		}
	}
	if got.Results[0].Score <= got.Results[2].Score {
		t.Errorf("scores must decrease: %v", got.Results)
	}
}

func TestMockRerankProvider_TopNCapsResults(t *testing.T) {
	p := NewMockRerankProvider()
	got, err := p.Rerank(context.Background(), RerankRequest{
		Query:     "alpha",
		Documents: []string{"alpha one", "beta", "alpha two", "gamma"},
		TopN:      2,
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("TopN=2 must return 2 results, got %d", len(got.Results))
	}
}

// TestMockRerankProvider_TiesKeepInputOrder pins the stable sort. An unstable
// sort would make equal-scoring documents come back in an arbitrary order,
// turning any test that asserts a full ordering into a flake.
func TestMockRerankProvider_TiesKeepInputOrder(t *testing.T) {
	p := NewMockRerankProvider()
	docs := []string{"same", "same", "same", "same"}
	for range 20 {
		got, err := p.Rerank(context.Background(), RerankRequest{Query: "same", Documents: docs})
		if err != nil {
			t.Fatalf("rerank: %v", err)
		}
		for i, r := range got.Results {
			if r.Index != i {
				t.Fatalf("tied documents must keep input order, got %v", indices(got.Results))
			}
		}
	}
}

// TestRerank_NoDocumentsIsNotAnError pins the contract that matters most to a
// caller: an upstream search finding nothing is normal, and forcing every
// caller to tell that apart from "the reranker is down" would be the wrong
// default.
func TestRerank_NoDocumentsIsNotAnError(t *testing.T) {
	p := NewMockRerankProvider()
	got, err := p.Rerank(context.Background(), RerankRequest{Query: "anything"})
	if err != nil {
		t.Fatalf("an empty candidate list must not be an error: %v", err)
	}
	if len(got.Results) != 0 {
		t.Errorf("expected no results, got %d", len(got.Results))
	}
}

// TestRerankWithEmptyCheck_RejectsAnEmptyQuery is the opposite case: ranking
// against nothing is meaningless, and returning the input order unchanged
// would look exactly like a reranker that had run and agreed.
func TestRerankWithEmptyCheck_RejectsAnEmptyQuery(t *testing.T) {
	b := NewBaseRerankProvider("test-rerank", "m", "http://localhost", 10, 0)
	_, err := b.RerankWithEmptyCheck(context.Background(),
		RerankRequest{Documents: []string{"a"}},
		func(context.Context, RerankRequest, string) (RerankResponse, error) {
			t.Fatal("the inner rerank must not run for an empty query")
			return RerankResponse{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected an empty-query error, got %v", err)
	}
}

// TestRerankWithEmptyCheck_RejectsOversizedBatches proves the cap is enforced
// before the call rather than surfacing as a vendor 400 the caller has to
// interpret.
func TestRerankWithEmptyCheck_RejectsOversizedBatches(t *testing.T) {
	b := NewBaseRerankProvider("test-rerank", "m", "http://localhost", 2, 0)
	_, err := b.RerankWithEmptyCheck(context.Background(),
		RerankRequest{Query: "q", Documents: []string{"a", "b", "c"}},
		func(context.Context, RerankRequest, string) (RerankResponse, error) {
			t.Fatal("the inner rerank must not run for an oversized batch")
			return RerankResponse{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "exceeds the provider maximum") {
		t.Fatalf("expected an oversized-batch error, got %v", err)
	}
}

func TestRerankWithEmptyCheck_ResolvesTheModel(t *testing.T) {
	b := NewBaseRerankProvider("test-rerank", "default-model", "http://localhost", 10, 0)

	var sawModel string
	capture := func(_ context.Context, _ RerankRequest, model string) (RerankResponse, error) {
		sawModel = model
		return RerankResponse{}, nil
	}

	if _, err := b.RerankWithEmptyCheck(context.Background(),
		RerankRequest{Query: "q", Documents: []string{"a"}}, capture); err != nil {
		t.Fatal(err)
	}
	if sawModel != "default-model" {
		t.Errorf("model = %q, want the provider default", sawModel)
	}

	if _, err := b.RerankWithEmptyCheck(context.Background(),
		RerankRequest{Query: "q", Documents: []string{"a"}, Model: "override"}, capture); err != nil {
		t.Fatal(err)
	}
	if sawModel != "override" {
		t.Errorf("model = %q, want the request override", sawModel)
	}
}

// TestMockRerankProvider_HonorsContextCancellation matters because a caller
// testing its own deadline handling against the mock should see what it would
// see live, not a call that always succeeds.
func TestMockRerankProvider_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewMockRerankProvider()
	_, err := p.Rerank(ctx, RerankRequest{Query: "q", Documents: []string{"a"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMockRerankProvider_HandlerReplacesScoring(t *testing.T) {
	want := errors.New("reranker unavailable")
	p := NewMockRerankProvider(WithMockRerankHandler(
		func(context.Context, RerankRequest) (RerankResponse, error) {
			return RerankResponse{}, want
		}))

	if _, err := p.Rerank(context.Background(),
		RerankRequest{Query: "q", Documents: []string{"a"}}); !errors.Is(err, want) {
		t.Fatalf("the handler must replace the built-in scoring, got %v", err)
	}
}

// TestCreateRerankProviderFromSpec_BuildsTheMock exercises the seam a
// constructor test cannot: a factory that was never registered — a missing
// import, an init() that did not run — fails here and nowhere else.
func TestCreateRerankProviderFromSpec_BuildsTheMock(t *testing.T) {
	rp, err := CreateRerankProviderFromSpec(RerankProviderSpec{ID: "rr", Type: "mock"})
	if err != nil {
		t.Fatalf("mock must be resolvable through the factory: %v", err)
	}
	if rp.ID() != "rr" {
		t.Errorf("ID = %q, want the spec's ID", rp.ID())
	}
	if rp.Type() != "rerank" {
		t.Errorf("Type = %q, want rerank", rp.Type())
	}
}

func TestCreateRerankProviderFromSpec_UnknownTypeListsWhatIsRegistered(t *testing.T) {
	_, err := CreateRerankProviderFromSpec(RerankProviderSpec{Type: "nope"})
	if err == nil {
		t.Fatal("expected an error for an unregistered type")
	}
	// The list is the actionable part: it tells the caller whether they
	// mistyped or forgot the provider's import.
	if !strings.Contains(err.Error(), "mock") {
		t.Errorf("the error should name the registered types, got %q", err)
	}
}

func TestRegisteredRerankProviderTypes_IncludesTheBundledBackends(t *testing.T) {
	got := RegisteredRerankProviderTypes()
	if !contains(got, "mock") {
		t.Errorf("mock must be registered by this package's init, got %v", got)
	}
	// Sorted, so callers can present the list without re-sorting.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("types must be sorted, got %v", got)
		}
	}
}

// TestResolveRerankTransport_RejectsPlatformHosting pins the deliberate gap:
// no hyperscaler exposes a first-party rerank endpoint, so a declared platform
// must fail loudly rather than fall through to the direct API and surface
// later as a confusing auth error.
func TestResolveRerankTransport_RejectsPlatformHosting(t *testing.T) {
	_, err := ResolveRerankTransport(RerankProviderSpec{ID: "rr", Type: "voyageai", Platform: "azure"})
	if err == nil || !strings.Contains(err.Error(), "not supported for the rerank role") {
		t.Fatalf("expected a platform rejection, got %v", err)
	}
}

func TestResolveRerankTransport_PassesThroughBaseURL(t *testing.T) {
	tr, err := ResolveRerankTransport(RerankProviderSpec{Type: "voyageai", BaseURL: "http://localhost:9"})
	if err != nil {
		t.Fatal(err)
	}
	if tr.BaseURL != "http://localhost:9" {
		t.Errorf("BaseURL = %q", tr.BaseURL)
	}
}

func TestClampTopN(t *testing.T) {
	for _, tc := range []struct{ topN, available, want int }{
		{0, 5, 5},  // unset means everything
		{3, 5, 3},  // cap applies
		{9, 5, 5},  // never more than available
		{-1, 5, 5}, // nonsense is treated as unset
	} {
		if got := ClampTopN(tc.topN, tc.available); got != tc.want {
			t.Errorf("ClampTopN(%d, %d) = %d, want %d", tc.topN, tc.available, got, tc.want)
		}
	}
}

func indices(rs []RankedDocument) []int {
	out := make([]int, len(rs))
	for i, r := range rs {
		out[i] = r.Index
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func TestBaseRerankProvider_Accessors(t *testing.T) {
	b := NewBaseRerankProvider("acme-rerank", "acme-1", "https://api.acme.test", 25, time.Second)

	if b.ID() != "acme-rerank" {
		t.Errorf("ID = %q", b.ID())
	}
	if b.Model() != "acme-1" {
		t.Errorf("Model = %q", b.Model())
	}
	if b.MaxDocuments() != 25 {
		t.Errorf("MaxDocuments = %d", b.MaxDocuments())
	}
	// base.Provider comes from the embedded Implementation; a provider that
	// did not satisfy it would not compile as a RerankProvider, but the type
	// is worth pinning because it is what identifies the role in telemetry.
	if b.Type() != "rerank" {
		t.Errorf("Type = %q, want rerank", b.Type())
	}
	if b.Name() != "acme-rerank" {
		t.Errorf("Name = %q", b.Name())
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if err := b.Init(context.Background()); err != nil {
		t.Errorf("Init: %v", err)
	}
	if err := b.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestBaseRerankProvider_DoRerankRequest_WrapsTransportFailures pins the
// reason rerank shares the embedding path's HTTP helper rather than calling
// http.Client directly: ProviderTransportError is what keeps a
// credential-bearing URL out of the error text (#1871).
func TestBaseRerankProvider_DoRerankRequest_WrapsTransportFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	b := NewBaseRerankProvider("acme-rerank", "m", srv.URL, 10, time.Second)
	b.APIKey = "secret"

	body, err := b.DoRerankRequest(context.Background(), HTTPRequestConfig{
		URL: srv.URL, Body: []byte(`{}`), UseAPIKey: true,
	})
	if err != nil {
		t.Fatalf("DoRerankRequest: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("body = %q", body)
	}

	// An unreachable host must produce the redacting wrapper, not a bare error.
	b.BaseURL = "http://127.0.0.1:1"
	_, err = b.DoRerankRequest(context.Background(), HTTPRequestConfig{
		URL: "http://127.0.0.1:1?api_key=secret", Body: []byte(`{}`),
	})
	var transportErr *ProviderTransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("expected a ProviderTransportError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("the error must not leak the credential: %q", err)
	}
}

func TestBaseRerankProvider_DoRerankRequest_SurfacesHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`nope`))
	}))
	defer srv.Close()

	b := NewBaseRerankProvider("acme-rerank", "m", srv.URL, 10, time.Second)
	_, err := b.DoRerankRequest(context.Background(), HTTPRequestConfig{
		URL: srv.URL, Body: []byte(`{}`),
	})
	var httpErr *ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected a ProviderHTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", httpErr.StatusCode)
	}
}

// TestLogRerankRequest_RecordsTheFieldsATraceNeeds captures the emitted record
// rather than merely calling the function. The provider, model and document
// count are what make a rerank identifiable in a trace next to the embedding
// calls around it; a log line missing them is indistinguishable from any other
// provider call and is the reason this is worth asserting at all.
func TestLogRerankRequest_RecordsTheFieldsATraceNeeds(t *testing.T) {
	var buf bytes.Buffer
	captured := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	restore := logger.GetLogger()
	logger.SetLogger(captured)
	t.Cleanup(func() { logger.SetLogger(restore) })

	LogRerankRequest("acme", "acme-1", 3, 120, time.Now().Add(-50*time.Millisecond))

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("no log record captured (%v): %q", err, buf.String())
	}
	if rec["provider"] != "acme" {
		t.Errorf("provider = %v", rec["provider"])
	}
	if rec["model"] != "acme-1" {
		t.Errorf("model = %v", rec["model"])
	}
	if rec["documents"] != float64(3) {
		t.Errorf("documents = %v, want 3", rec["documents"])
	}
	if rec["tokens"] != float64(120) {
		t.Errorf("tokens = %v, want 120", rec["tokens"])
	}
	// The duration is measured from the caller's start time, so it must
	// reflect elapsed time rather than being stamped as zero.
	if ms, ok := rec["duration_ms"].(float64); !ok || ms < 1 {
		t.Errorf("duration_ms = %v, want the elapsed time since start", rec["duration_ms"])
	}
}

func TestMockRerankProvider_MaxDocumentsIsConfigurable(t *testing.T) {
	p := NewMockRerankProvider(WithMockRerankMaxDocuments(2))
	if p.MaxDocuments() != 2 {
		t.Errorf("MaxDocuments = %d, want 2", p.MaxDocuments())
	}
}

func TestCreateRerankProviderFromSpec_MockHonorsAdditionalConfig(t *testing.T) {
	rp, err := CreateRerankProviderFromSpec(RerankProviderSpec{
		ID: "rr", Type: "mock",
		AdditionalConfig: map[string]any{"max_documents": 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rp.MaxDocuments() != 7 {
		t.Errorf("MaxDocuments = %d, want the configured 7", rp.MaxDocuments())
	}
}
