// Package inference defines a single generic Provider interface for every
// classification-shaped inference call PromptKit makes against a vendor API
// (zero-shot text classification, topic control, moderation, and similar
// label-scoring tasks).
//
// The design rule is one (role, type) = one vendor API: a Provider is a thin
// codec over that API's wire shape (Request in, Response out) and nothing
// more. It does not know what task it is serving — "is this on-topic",
// "is this toxic", "what emotion is this" are all the same shape of call
// (candidate labels in, scored labels out) to the vendor, so they share one
// interface here. Task semantics — which labels to ask for, how to interpret
// the highest-scoring label, what threshold makes a guardrail fail closed —
// live entirely in the callers (the eval/guardrail handlers), never in a
// Provider implementation.
package inference

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// LabelScore is a single candidate label and the provider's score for it.
type LabelScore struct {
	Label string
	Score float64
}

// Usage carries billing/telemetry data for one Infer call.
type Usage struct {
	InputTokens int
	Cost        float64 // USD, when the API reports it
}

// Request is the vendor-agnostic shape of an inference call. Not every
// field applies to every provider: a fixed-head classifier (e.g. a
// dedicated moderation endpoint) has no use for Labels or Prompt and
// rejects a non-empty Prompt; a zero-shot provider requires Labels.
type Request struct {
	// Model overrides the provider's configured model.
	Model string
	// Inputs is the content under judgment; roles are preserved.
	Inputs []types.Message
	// Labels lists candidate answers; empty means the model's own label set.
	Labels []string
	// Prompt is instruction text. Fixed-head backends reject a non-empty Prompt.
	Prompt string
	// Params carries API-level tweaks (e.g. "multi_label": true).
	Params map[string]any
}

// Response is the vendor-agnostic shape of an inference result.
type Response struct {
	Model  string
	Scores []LabelScore // probabilities, highest first
	Usage  Usage
	Raw    string // provider's unparsed answer, for diagnostics
}

// Score returns the probability for label (case-insensitive) and whether it
// was present in the response.
func (r Response) Score(label string) (float64, bool) {
	for _, s := range r.Scores {
		if strings.EqualFold(s.Label, label) {
			return s.Score, true
		}
	}
	return 0, false
}

// Provider is implemented by every inference backend: one vendor API,
// codec'd to and from the shapes above.
type Provider interface {
	Infer(ctx context.Context, req Request) (Response, error)
}
