package handlers

import (
	"context"
	"fmt"
	"math"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// CosineSimilarityHandler computes cosine similarity between
// embeddings. Params: reference []float64, min_similarity float64.
// Target embedding comes from Metadata["embedding"].
type CosineSimilarityHandler struct{}

// Type returns the eval type identifier.
func (h *CosineSimilarityHandler) Type() string {
	return "cosine_similarity"
}

// Eval computes cosine similarity and checks against threshold.
func (h *CosineSimilarityHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	reference, ok := extractFloat64Slice(params, "reference")
	if !ok || len(reference) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no reference embedding specified",
		}, nil
	}

	minSim, ok := extractFloat64(params, "min_similarity")
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no min_similarity specified",
		}, nil
	}

	target, ok := extractFloat64Slice(
		evalCtx.Metadata, "embedding",
	)
	if !ok || len(target) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no embedding found in metadata",
		}, nil
	}

	return h.computeAndCheck(reference, target, minSim)
}

// computeAndCheck calculates similarity and builds the result.
func (h *CosineSimilarityHandler) computeAndCheck(
	reference, target []float64, minSim float64,
) (result *evals.EvalResult, err error) {
	if len(reference) != len(target) {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"dimension mismatch: reference=%d, target=%d",
				len(reference), len(target),
			),
		}, nil
	}

	similarity := cosineSimilarity(reference, target)
	passed := similarity >= minSim
	explanation := fmt.Sprintf(
		"cosine similarity %.4f vs threshold %.4f",
		similarity, minSim,
	)

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Score:       &similarity,
		Explanation: explanation,
	}, nil
}

// cosineSimilarity computes the cosine similarity between two
// vectors.
func cosineSimilarity(a, b []float64) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// extractFloat64Slice extracts a []float64 from a map value.
func extractFloat64Slice(
	m map[string]any, key string,
) (result []float64, ok bool) {
	v, exists := m[key]
	if !exists {
		return nil, false
	}

	switch s := v.(type) {
	case []float64:
		return s, true
	case []any:
		return convertToFloat64Slice(s)
	default:
		return nil, false
	}
}

// convertToFloat64Slice converts a []any to []float64.
func convertToFloat64Slice(
	s []any,
) (result []float64, ok bool) {
	out := make([]float64, 0, len(s))
	for _, item := range s {
		switch n := item.(type) {
		case float64:
			out = append(out, n)
		case float32:
			out = append(out, float64(n))
		case int:
			out = append(out, float64(n))
		default:
			return nil, false
		}
	}
	return out, true
}
