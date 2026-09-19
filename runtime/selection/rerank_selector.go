package selection

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// DefaultRerankTimeout bounds one selection round's rerank calls.
//
// Selection sits on the turn's critical path — the skill path runs once per
// Send, the tool path once per provider round — so an unbounded call lets a
// stalled reranker hold the whole turn open. The fallback when it expires is
// cheap by construction: Select returns an error and PromptKit includes all
// eligible candidates, which is what would have happened without a selector.
const DefaultRerankTimeout = 5 * time.Second

// RerankSelector narrows a candidate set with a rerank provider: it ranks
// each candidate's description against the turn's query and returns the
// best IDs in order.
//
// A note on where to use it. The skill path selects once per Send and is the
// safer first target. The tool path selects once per provider round, and
// changing the tool set between rounds invalidates the provider's cached
// prompt prefix — a per-round rerank can cost more tokens than the narrowing
// saves, so measure before turning it on there.
//
// Scores never leave this type. PromptKit's selector boundary takes IDs only,
// and rerank scores are not comparable across providers or models anyway.
type RerankSelector struct {
	rerank  providers.RerankProvider
	timeout time.Duration
}

// RerankSelectorOption configures a RerankSelector.
type RerankSelectorOption func(*RerankSelector)

// WithRerankTimeout overrides DefaultRerankTimeout. A non-positive value
// leaves the default in place rather than disabling the bound.
func WithRerankTimeout(d time.Duration) RerankSelectorOption {
	return func(s *RerankSelector) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// NewRerankSelector creates a selector that ranks with the rerank provider
// handed to Init.
func NewRerankSelector(opts ...RerankSelectorOption) *RerankSelector {
	s := &RerankSelector{timeout: DefaultRerankTimeout}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name identifies the selector for registration and config.
func (s *RerankSelector) Name() string { return "rerank" }

// Init takes the rerank provider from the context, and fails without one.
//
// Failing is the point: Select's fallback is "include all eligible", so a
// selector that degraded silently would be indistinguishable from one that
// ran and chose everything.
func (s *RerankSelector) Init(ctx SelectorContext) error {
	if ctx.Rerank == nil {
		return fmt.Errorf("rerank selector: no rerank provider configured " +
			"(set SelectorContext.Rerank, e.g. via sdk.WithRerankProvider)")
	}
	s.rerank = ctx.Rerank
	return nil
}

// Select orders candidates by relevance to q.Text and returns their IDs,
// best first, at most q.K of them.
//
// An error here means PromptKit falls back to including every eligible
// candidate. That is the caller's policy, not this selector's, so a provider
// failure is reported rather than swallowed.
func (s *RerankSelector) Select(
	ctx context.Context, q Query, candidates []Candidate,
) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	docs := make([]string, len(candidates))
	for i, c := range candidates {
		// Description is what a reranker can actually score; the ID is
		// PromptKit's handle. Name is the fallback because an empty
		// document ranks against nothing.
		docs[i] = c.Description
		if docs[i] == "" {
			docs[i] = c.Name
		}
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	ranked, err := s.rankAll(ctx, q, docs)
	if err != nil {
		return nil, err
	}

	// Best first. A stable sort keeps the provider's own ordering among
	// equal scores instead of inventing one.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	ids := make([]string, 0, len(ranked))
	for _, r := range ranked {
		if r.Index < 0 || r.Index >= len(candidates) {
			continue // a provider that returns a bogus index loses that result, not the round
		}
		ids = append(ids, candidates[r.Index].ID)
		if q.K > 0 && len(ids) == q.K {
			break
		}
	}
	return ids, nil
}

// rankAll ranks docs in as many calls as the provider's document cap
// requires, returning results indexed against the full docs slice.
//
// Batching rather than truncating: a set too large for one call is exactly
// the case with the most to narrow, and one over-sized call is an error,
// which PromptKit reads as "include all eligible" — selection switching
// itself off precisely when it is most useful.
//
// Merging across batches compares scores from separate calls. That is sound
// here and only here: the scores come from one provider and one model, which
// is the scope RerankProvider documents them as comparable within.
func (s *RerankSelector) rankAll(
	ctx context.Context, q Query, docs []string,
) ([]providers.RankedDocument, error) {
	batch := s.rerank.MaxDocuments()
	if batch <= 0 || batch >= len(docs) {
		resp, err := s.rerank.Rerank(ctx, providers.RerankRequest{
			Query: q.Text, Documents: docs, TopN: q.K,
		})
		if err != nil {
			return nil, err
		}
		return resp.Results, nil
	}

	var all []providers.RankedDocument
	for start := 0; start < len(docs); start += batch {
		end := min(start+batch, len(docs))
		resp, err := s.rerank.Rerank(ctx, providers.RerankRequest{
			Query: q.Text, Documents: docs[start:end], TopN: q.K,
		})
		if err != nil {
			return nil, fmt.Errorf("rerank batch %d-%d: %w", start, end, err)
		}
		// Indices come back relative to the batch; shift them so the
		// caller can key on them against the full candidate slice.
		for _, r := range resp.Results {
			r.Index += start
			all = append(all, r)
		}
	}
	return all, nil
}

// Compile-time proof the selector satisfies the interface PromptKit calls.
var _ Selector = (*RerankSelector)(nil)
