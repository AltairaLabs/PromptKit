// Package corpus provides a reference [memory.Retriever] over a fixed set of
// documents — a knowledge base the host supplies, deliberately separate from
// the [memory.Store] the memory tools read and write.
//
// That separation is the point. The two retrieval paths in PromptKit answer
// different questions:
//
//   - The memory tools (memory__remember / memory__recall) let the model
//     manage facts about the *subject* — what the user told it to remember.
//     The model decides when to look, and the store is scoped per subject.
//   - Ambient injection asks a corpus what is relevant to *this turn* and puts
//     the answer in the system prompt before the model runs. The model never
//     decides; it simply sees grounding it did not have to ask for.
//
// Pointing ambient injection at the memory store would collapse the two into
// one confusing path — the model would find the same rows twice, once by
// asking and once without. Retrieval for grounding belongs over the host's own
// content: documentation, a product catalog, a support knowledge base.
//
// This implementation scores documents by term overlap with the latest user
// turn. That is enough to develop and test against, and deliberately not a
// search engine: production hosts implement [memory.Retriever] against a
// vector index or a real search backend.
package corpus

import (
	"context"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	// MemoryType is the [memory.Memory] Type given to every retrieved
	// document, so a context formatter (or a host reading the injected set)
	// can tell grounding documents apart from remembered facts.
	MemoryType = "document"

	// defaultTopK is how many documents are injected when the host does not
	// say — enough to ground an answer without crowding the system prompt.
	defaultTopK = 3

	// minTermLength drops one- and two-character tokens, which are common
	// short words ("do", "is") that would otherwise match everything.
	minTermLength = 2
)

// Document is one unit of retrievable host content.
type Document struct {
	// ID identifies the document; it is carried onto the retrieved
	// memory so a prompt can cite it.
	ID string
	// Title is a human-readable label, surfaced in metadata.
	Title string
	// Text is the content injected into the prompt.
	Text string
}

// Retriever answers ambient-injection queries from a fixed document set.
type Retriever struct {
	docs []Document
	topK int
}

// Option configures a [Retriever].
type Option func(*Retriever)

// WithTopK caps how many documents are injected. Defaults to 3 — enough to
// ground an answer without crowding the system prompt.
func WithTopK(n int) Option {
	return func(r *Retriever) { r.topK = n }
}

// New returns a [Retriever] over docs.
func New(docs []Document, opts ...Option) *Retriever {
	r := &Retriever{docs: docs, topK: defaultTopK}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RetrieveContext implements [memory.Retriever]. The query is the latest user
// turn — what the assistant is about to answer.
//
// A turn with no user text, or one sharing no term with any document, returns
// nothing rather than everything: injecting an unrelated document is worse
// than injecting none, because the model cannot tell that it was not chosen.
func (r *Retriever) RetrieveContext(
	_ context.Context, _ map[string]string, messages []types.Message,
) ([]*memory.Memory, error) {
	terms := tokenize(latestUserText(messages))
	if len(terms) == 0 {
		return nil, nil
	}

	type scored struct {
		doc   Document
		score int
		order int
	}
	var hits []scored
	for i, doc := range r.docs {
		if score := overlap(terms, tokenize(doc.Title+" "+doc.Text)); score > 0 {
			hits = append(hits, scored{doc: doc, score: score, order: i})
		}
	}
	if len(hits) == 0 {
		return nil, nil
	}

	// Highest overlap first; declaration order breaks ties so results are
	// stable across runs (an unstable prompt defeats provider caching).
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].order < hits[b].order
	})
	if r.topK > 0 && len(hits) > r.topK {
		hits = hits[:r.topK]
	}

	out := make([]*memory.Memory, 0, len(hits))
	for _, h := range hits {
		out = append(out, &memory.Memory{
			ID:         h.doc.ID,
			Type:       MemoryType,
			Content:    h.doc.Text,
			Confidence: 1.0,
			Metadata:   map[string]any{"title": h.doc.Title},
		})
	}
	return out, nil
}

// latestUserText returns the text of the most recent user message, reading
// both Content and text Parts — user text arrives in either depending on how
// the caller built the turn.
func latestUserText(messages []types.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if text := messages[i].GetContent(); text != "" {
			return text
		}
	}
	return ""
}

// tokenize lowercases and splits on non-letter runes, dropping one- and
// two-character tokens so common short words ("do", "is") don't match
// everything.
func tokenize(s string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if len(f) > minTermLength {
			out[stem(f)] = struct{}{}
		}
	}
	return out
}

// stem strips a trailing plural "s" so "refunds" matches "refund". Crude by
// design — a real implementation uses the search backend's analyzer.
func stem(word string) string {
	if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
		return word[:len(word)-1]
	}
	return word
}

// overlap counts terms present in both sets.
func overlap(query, doc map[string]struct{}) int {
	count := 0
	for term := range query {
		if _, ok := doc[term]; ok {
			count++
		}
	}
	return count
}
