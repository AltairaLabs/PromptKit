package handlers

import (
	"context"
	"fmt"
	"math"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// CosineSimilarityHandler computes cosine similarity between
// embeddings. Params: reference []float64. Target embedding comes from
// Metadata["embedding"].
//
// It is a pure eval primitive: Score is the raw similarity. A threshold
// belongs on the `type: assertion` / `type: guardrail` wrapper (min_score), so
// min_similarity, min_score and max_score are rejected here.
type CosineSimilarityHandler struct{}

// Type returns the eval type identifier.
func (h *CosineSimilarityHandler) Type() string {
	return "cosine_similarity"
}

// Eval computes the cosine similarity between the reference and the
// target embedding.
func (h *CosineSimilarityHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	if msg := rejectCosineThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	reference, ok := extractFloat64Slice(params, "reference")
	if !ok || len(reference) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: "no reference embedding specified",
		}, nil
	}

	target, ok := extractFloat64Slice(
		evalCtx.Metadata, "embedding",
	)
	if !ok || len(target) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: "no embedding found in metadata",
		}, nil
	}

	return h.compute(reference, target)
}

// compute calculates similarity and builds the result.
func (h *CosineSimilarityHandler) compute(
	reference, target []float64,
) (result *evals.EvalResult, err error) {
	if len(reference) != len(target) {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: boolScore(false),
			Explanation: fmt.Sprintf(
				"dimension mismatch: reference=%d, target=%d",
				len(reference), len(target),
			),
		}, nil
	}

	similarity := cosineSimilarity(reference, target)
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       &similarity,
		MetricValue: &similarity,
		Details:     map[string]any{"similarity": similarity},
		Explanation: fmt.Sprintf("cosine similarity %.4f", similarity),
	}, nil
}

// rejectCosineThresholdParams refuses a threshold on the eval. min_similarity
// was this check's own threshold param before #2354; it is named in the error
// so a pack that still sets it learns where the threshold went.
func rejectCosineThresholdParams(params map[string]any) string {
	if _, present := params["min_similarity"]; present {
		return "min_similarity is not a valid param on cosine_similarity; it reports the raw " +
			"similarity as its score. Wrap it with `type: assertion` and set min_score there " +
			"(see runtime/evals/wrappers.go)"
	}
	return rejectThresholdParams(params)
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
