package sdk

import (
	"context"
	"strings"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// applyOptions runs options against a bare config, which is what Open does
// before building anything. Rerank needs no pack or provider, so this keeps
// the tests to the wiring under test.
func applyRerankOptions(t *testing.T, opts ...Option) *config {
	t.Helper()
	c := &config{}
	for _, o := range opts {
		if err := o(c); err != nil {
			t.Fatalf("applying option: %v", err)
		}
	}
	return c
}

func TestWithRerankProvider_ConstructsAndRegisters(t *testing.T) {
	c := applyRerankOptions(t, WithRerankProvider(ProviderSpec{ID: "rr", Type: "mock"}))

	if len(c.rerankProviderIDs) != 1 || c.rerankProviderIDs[0] != "rr" {
		t.Fatalf("IDs = %v, want [rr]", c.rerankProviderIDs)
	}
	if c.rerankProviders["rr"] == nil {
		t.Fatal("provider not stored under its ID")
	}
}

func TestWithRerankProvider_UnknownTypeFailsAtConfigTime(t *testing.T) {
	c := &config{}
	err := WithRerankProvider(ProviderSpec{Type: "nope"})(c)
	if err == nil {
		t.Fatal("an unregistered type must fail when the option is applied")
	}
	if !strings.Contains(err.Error(), "WithRerankProvider") {
		t.Errorf("the error should name the option, got %q", err)
	}
}

// TestConversation_RerankProvider_ReturnsTheFirstDeclared pins the accessor
// that makes this role usable at all: nothing in the pipeline calls a
// reranker, so without a way to reach the constructed provider the whole
// option would be inert.
func TestConversation_RerankProvider_ReturnsTheFirstDeclared(t *testing.T) {
	c := applyRerankOptions(t,
		WithRerankProvider(ProviderSpec{ID: "first", Type: "mock"}),
		WithRerankProvider(ProviderSpec{ID: "second", Type: "mock"}),
	)
	conv := &Conversation{config: c}

	rp, err := conv.RerankProvider()
	if err != nil {
		t.Fatalf("RerankProvider: %v", err)
	}
	if rp.ID() != "first" {
		t.Errorf("default = %q, want the first declared", rp.ID())
	}

	byID, err := conv.RerankProviderByID("second")
	if err != nil {
		t.Fatalf("RerankProviderByID: %v", err)
	}
	if byID.ID() != "second" {
		t.Errorf("byID = %q", byID.ID())
	}

	if got := conv.RerankProviderIDs(); len(got) != 2 || got[0] != "first" {
		t.Errorf("IDs = %v, want declaration order", got)
	}
}

// TestConversation_RerankProvider_ErrorsWhenUnconfigured makes the miss
// obvious at the accessor rather than as a nil dereference at the call site.
func TestConversation_RerankProvider_ErrorsWhenUnconfigured(t *testing.T) {
	conv := &Conversation{config: &config{}}
	if _, err := conv.RerankProvider(); err == nil {
		t.Fatal("expected an error when no rerank provider is configured")
	}
	if _, err := conv.RerankProviderByID("nope"); err == nil {
		t.Fatal("expected an error for an unknown ID")
	}
}

func TestConversation_RerankProvider_NilSafe(t *testing.T) {
	var conv *Conversation
	if _, err := conv.RerankProvider(); err == nil {
		t.Fatal("a nil conversation must error, not panic")
	}
	if got := conv.RerankProviderIDs(); got != nil {
		t.Errorf("IDs on a nil conversation = %v, want nil", got)
	}
}

// TestApplyProviderConfig_RoutesRerankRole covers the role dispatch. A role
// that validates but has no route falls into applyProviderConfig's default
// branch, which is exactly the inert-declaration shape: the YAML is accepted
// and nothing is built.
func TestApplyProviderConfig_RoutesRerankRole(t *testing.T) {
	c := &config{}
	err := c.applyProviderConfig(&pkgconfig.Provider{
		ID: "rr", Type: "mock", Role: pkgconfig.RoleRerank,
	})
	if err != nil {
		t.Fatalf("role: rerank must route: %v", err)
	}
	if c.rerankProviders["rr"] == nil {
		t.Fatal("a role: rerank provider file must produce a rerank provider")
	}
}

func TestRegisteredProviderTypes_ReportsRerank(t *testing.T) {
	got := RegisteredProviderTypes()
	types, ok := got[pkgconfig.RoleRerank]
	if !ok {
		t.Fatal("introspection must report the rerank role")
	}
	var sawMock, sawVoyage, sawCohere bool
	for _, ty := range types {
		switch ty {
		case "mock":
			sawMock = true
		case "voyageai":
			sawVoyage = true
		case "cohere":
			sawCohere = true
		}
	}
	if !sawMock || !sawVoyage || !sawCohere {
		t.Errorf("expected mock, voyageai and cohere to be registered, got %v", types)
	}
}

// TestRerankRoundTrip_ThroughTheSDK is the end-to-end check: configure by
// role, reach the provider through the accessor, and get a usable ordering
// back. Each half is covered above; this proves they meet.
func TestRerankRoundTrip_ThroughTheSDK(t *testing.T) {
	c := applyRerankOptions(t, WithRerankProvider(ProviderSpec{ID: "rr", Type: "mock"}))
	conv := &Conversation{config: c}

	rp, err := conv.RerankProvider()
	if err != nil {
		t.Fatal(err)
	}
	out, err := rp.Rerank(context.Background(), providers.RerankRequest{
		Query:     "refund",
		Documents: []string{"shipping info", "refund terms"},
		TopN:      1,
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(out.Results) != 1 || out.Results[0].Index != 1 {
		t.Errorf("expected the refund document first, got %+v", out.Results)
	}
}
