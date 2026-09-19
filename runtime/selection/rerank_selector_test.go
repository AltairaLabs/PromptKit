package selection

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// rankedBy returns a mock whose handler ranks documents by an explicit list of
// document texts, best first. Anything not named is dropped, which is what a
// real reranker does when TopN cuts the tail.
func rankedBy(order ...string) *providers.MockRerankProvider {
	return providers.NewMockRerankProvider(
		providers.WithMockRerankHandler(
			func(_ context.Context, req providers.RerankRequest) (providers.RerankResponse, error) {
				var out []providers.RankedDocument
				for rank, want := range order {
					for i, doc := range req.Documents {
						if doc == want {
							out = append(out, providers.RankedDocument{
								Index: i,
								Score: float64(len(order) - rank),
							})
						}
					}
				}
				return providers.RerankResponse{Results: out}, nil
			}),
	)
}

func candidates() []Candidate {
	return []Candidate{
		{ID: "refund", Description: "issue a refund"},
		{ID: "lookup", Description: "look up an order"},
		{ID: "escalate", Description: "escalate to a human"},
	}
}

// The selector's whole job: hand the ranker the descriptions, hand PromptKit
// back the IDs, in the ranker's order. Input order must not survive.
func TestRerankSelector_ReturnsIDsInRankedOrder(t *testing.T) {
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{
		Rerank: rankedBy("escalate to a human", "issue a refund", "look up an order"),
	}))

	got, err := s.Select(context.Background(), Query{Text: "angry customer"}, candidates())

	require.NoError(t, err)
	assert.Equal(t, []string{"escalate", "refund", "lookup"}, got)
}

// A selector that needs a provider it did not get must fail Init. Select's
// fallback is "include all eligible", so a selector that degraded silently
// would be indistinguishable from one that ran and chose everything.
func TestRerankSelector_InitFailsWithoutProvider(t *testing.T) {
	err := NewRerankSelector().Init(SelectorContext{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rerank")
}

// Query.K is the caller's cap. It has to reach the provider as TopN — a
// selector that ranked everything and trimmed afterwards would pay for
// candidates it then threw away.
func TestRerankSelector_PassesKAsTopN(t *testing.T) {
	var gotTopN int
	prov := providers.NewMockRerankProvider(providers.WithMockRerankHandler(
		func(_ context.Context, req providers.RerankRequest) (providers.RerankResponse, error) {
			gotTopN = req.TopN
			return providers.RerankResponse{
				Results: []providers.RankedDocument{{Index: 0, Score: 1}},
			}, nil
		}))
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	got, err := s.Select(context.Background(), Query{Text: "q", K: 2}, candidates())

	require.NoError(t, err)
	assert.Equal(t, 2, gotTopN, "K must reach the provider, not be applied after")
	assert.Equal(t, []string{"refund"}, got)
}

// Descriptions are what gets ranked — the ID is PromptKit's handle, not
// something a reranker can score.
func TestRerankSelector_RanksDescriptionsNotIDs(t *testing.T) {
	var gotDocs []string
	prov := providers.NewMockRerankProvider(providers.WithMockRerankHandler(
		func(_ context.Context, req providers.RerankRequest) (providers.RerankResponse, error) {
			gotDocs = req.Documents
			return providers.RerankResponse{}, nil
		}))
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	_, err := s.Select(context.Background(), Query{Text: "q"}, candidates())

	require.NoError(t, err)
	assert.Equal(t, []string{"issue a refund", "look up an order", "escalate to a human"}, gotDocs)
}

// A candidate with no description still has to be rankable — falling back to
// the name beats sending an empty string, which ranks against nothing.
func TestRerankSelector_FallsBackToNameWhenDescriptionEmpty(t *testing.T) {
	var gotDocs []string
	prov := providers.NewMockRerankProvider(providers.WithMockRerankHandler(
		func(_ context.Context, req providers.RerankRequest) (providers.RerankResponse, error) {
			gotDocs = req.Documents
			return providers.RerankResponse{}, nil
		}))
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	_, err := s.Select(context.Background(), Query{Text: "q"}, []Candidate{
		{ID: "a", Name: "refund_order", Description: ""},
		{ID: "b", Name: "lookup", Description: "look up an order"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"refund_order", "look up an order"}, gotDocs)
}

// Nothing to rank is not worth a network call.
func TestRerankSelector_NoCandidatesMakesNoCall(t *testing.T) {
	called := false
	prov := providers.NewMockRerankProvider(providers.WithMockRerankHandler(
		func(_ context.Context, _ providers.RerankRequest) (providers.RerankResponse, error) {
			called = true
			return providers.RerankResponse{}, nil
		}))
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	got, err := s.Select(context.Background(), Query{Text: "q"}, nil)

	require.NoError(t, err)
	assert.Empty(t, got)
	assert.False(t, called, "no candidates means no provider call")
}

// manyCandidates builds n candidates whose descriptions are "doc-<i>".
func manyCandidates(n int) []Candidate {
	out := make([]Candidate, n)
	for i := range out {
		out[i] = Candidate{ID: fmt.Sprintf("id-%d", i), Description: fmt.Sprintf("doc-%d", i)}
	}
	return out
}

// A candidate set larger than the provider's cap must still get ranked. The
// alternative — one over-sized call — is an error from the provider, which
// PromptKit reads as "include all eligible": selection would switch itself off
// exactly when there is most to narrow.
func TestRerankSelector_BatchesBeyondMaxDocuments(t *testing.T) {
	var batches [][]string
	prov := providers.NewMockRerankProvider(
		providers.WithMockRerankMaxDocuments(2),
		providers.WithMockRerankHandler(
			func(_ context.Context, req providers.RerankRequest) (providers.RerankResponse, error) {
				batches = append(batches, req.Documents)
				out := make([]providers.RankedDocument, len(req.Documents))
				for i, doc := range req.Documents {
					// Score by the document's GLOBAL number, so the two best
					// candidates live in the last two batches. A merge that
					// kept only the first batch, or that let batch-local
					// scores stand, would not produce them.
					var n int
					_, _ = fmt.Sscanf(doc, "doc-%d", &n)
					out[i] = providers.RankedDocument{Index: i, Score: float64(n)}
				}
				return providers.RerankResponse{Results: out}, nil
			}))
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	got, err := s.Select(context.Background(), Query{Text: "q", K: 2}, manyCandidates(5))

	require.NoError(t, err)
	require.Len(t, batches, 3, "5 candidates at a cap of 2 is three calls")
	assert.Equal(t, []string{"doc-0", "doc-1"}, batches[0])
	assert.Equal(t, []string{"doc-4"}, batches[2], "the remainder is its own call")
	assert.Equal(t, []string{"id-4", "id-3"}, got,
		"results merge across batches by score, then K applies to the merged set")
}

// A provider error is the selector's to report. PromptKit turns it into
// "include all eligible" — that fallback belongs to the caller, not here.
func TestRerankSelector_ReturnsProviderError(t *testing.T) {
	prov := providers.NewMockRerankProvider(providers.WithMockRerankHandler(
		func(_ context.Context, _ providers.RerankRequest) (providers.RerankResponse, error) {
			return providers.RerankResponse{}, errors.New("rerank upstream down")
		}))
	s := NewRerankSelector()
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	_, err := s.Select(context.Background(), Query{Text: "q"}, candidates())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rerank upstream down")
}

// Every Send that selects gains a synchronous network call, so the selector
// bounds it rather than letting a hung reranker hold the turn open.
func TestRerankSelector_AppliesTimeout(t *testing.T) {
	prov := providers.NewMockRerankProvider(providers.WithMockRerankHandler(
		func(ctx context.Context, _ providers.RerankRequest) (providers.RerankResponse, error) {
			<-ctx.Done()
			return providers.RerankResponse{}, ctx.Err()
		}))
	s := NewRerankSelector(WithRerankTimeout(20 * time.Millisecond))
	require.NoError(t, s.Init(SelectorContext{Rerank: prov}))

	start := time.Now()
	_, err := s.Select(context.Background(), Query{Text: "q"}, candidates())

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second, "the timeout must actually bound the call")
}
