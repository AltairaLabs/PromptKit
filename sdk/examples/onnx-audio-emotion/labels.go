package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// softmax converts logits to a probability distribution, subtracting the
// max first for numerical stability.
func softmax(logits []float32) []float32 {
	if len(logits) == 0 {
		return nil
	}
	maxLogit := logits[0]
	for _, v := range logits {
		if v > maxLogit {
			maxLogit = v
		}
	}
	exps := make([]float64, len(logits))
	var sum float64
	for i, v := range logits {
		e := math.Exp(float64(v - maxLogit))
		exps[i] = e
		sum += e
	}
	out := make([]float32, len(logits))
	for i, e := range exps {
		out[i] = float32(e / sum)
	}
	return out
}

// labelScores softmaxes the model logits and pairs each probability with
// its label, returning the pairs sorted by descending score. labels must
// be in the model's output-index order.
func labelScores(logits []float32, labels []string) ([]classify.LabelScore, error) {
	if len(logits) != len(labels) {
		return nil, fmt.Errorf("logits (%d) and labels (%d) length mismatch", len(logits), len(labels))
	}
	probs := softmax(logits)
	out := make([]classify.LabelScore, len(labels))
	for i, label := range labels {
		out[i] = classify.LabelScore{Label: label, Score: float64(probs[i])}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	return out, nil
}
