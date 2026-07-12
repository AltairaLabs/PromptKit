package main

import (
	"math"
	"testing"
)

func TestSoftmax_SumsToOne(t *testing.T) {
	out := softmax([]float32{2, 1, 0.1})
	var sum float64
	for _, v := range out {
		sum += float64(v)
	}
	if math.Abs(sum-1.0) > 1e-5 {
		t.Errorf("softmax sum = %f, want 1.0", sum)
	}
	if !(out[0] > out[1] && out[1] > out[2]) {
		t.Errorf("softmax not monotonic with logits: %v", out)
	}
}

func TestLabelScores_SortedDescending(t *testing.T) {
	scores, err := labelScores([]float32{0.1, 3.0, 1.0}, []string{"neu", "ang", "hap"})
	if err != nil {
		t.Fatalf("labelScores: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("len = %d, want 3", len(scores))
	}
	if scores[0].Label != "ang" {
		t.Errorf("top label = %q, want ang", scores[0].Label)
	}
	if !(scores[0].Score >= scores[1].Score && scores[1].Score >= scores[2].Score) {
		t.Errorf("not sorted descending: %+v", scores)
	}
}

func TestLabelScores_LengthMismatch(t *testing.T) {
	if _, err := labelScores([]float32{0.1, 0.2}, []string{"only-one"}); err == nil {
		t.Fatal("expected error on logits/labels length mismatch, got nil")
	}
}
