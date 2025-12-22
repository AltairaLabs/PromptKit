package stage

import (
	"math"
	"sort"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{1.0, 2.0, 3.0}

	sim := CosineSimilarity(a, b)

	if math.Abs(sim-1.0) > 1e-6 {
		t.Errorf("Identical vectors should have similarity 1.0, got %f", sim)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1.0, 0.0, 0.0}
	b := []float32{0.0, 1.0, 0.0}

	sim := CosineSimilarity(a, b)

	if math.Abs(sim) > 1e-6 {
		t.Errorf("Orthogonal vectors should have similarity 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{-1.0, -2.0, -3.0}

	sim := CosineSimilarity(a, b)

	if math.Abs(sim-(-1.0)) > 1e-6 {
		t.Errorf("Opposite vectors should have similarity -1.0, got %f", sim)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1.0, 2.0}
	b := []float32{1.0, 2.0, 3.0}

	sim := CosineSimilarity(a, b)

	if sim != 0.0 {
		t.Errorf("Different length vectors should return 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_EmptyVectors(t *testing.T) {
	a := []float32{}
	b := []float32{}

	sim := CosineSimilarity(a, b)

	if sim != 0.0 {
		t.Errorf("Empty vectors should return 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0.0, 0.0, 0.0}
	b := []float32{1.0, 2.0, 3.0}

	sim := CosineSimilarity(a, b)

	if sim != 0.0 {
		t.Errorf("Zero vector should return 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_PartialSimilarity(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{1.0, 1.0}

	sim := CosineSimilarity(a, b)

	// Expected: 1*1 + 0*1 = 1, ||a|| = 1, ||b|| = sqrt(2)
	// cos = 1 / (1 * sqrt(2)) = 0.7071...
	expected := 1.0 / math.Sqrt(2)
	if math.Abs(sim-expected) > 1e-6 {
		t.Errorf("Expected similarity ~%f, got %f", expected, sim)
	}
}

func TestScoredMessages_SortByScore(t *testing.T) {
	messages := ScoredMessages{
		{Index: 0, Score: 0.5},
		{Index: 1, Score: 0.9},
		{Index: 2, Score: 0.3},
		{Index: 3, Score: 0.7},
	}

	sort.Sort(messages)

	// Should be sorted descending by score
	expectedOrder := []int{1, 3, 0, 2}
	for i, expected := range expectedOrder {
		if messages[i].Index != expected {
			t.Errorf("Position %d: expected index %d, got %d", i, expected, messages[i].Index)
		}
	}
}

func TestByOriginalIndex_Sort(t *testing.T) {
	messages := ByOriginalIndex{
		{Index: 3, Score: 0.5},
		{Index: 1, Score: 0.9},
		{Index: 4, Score: 0.3},
		{Index: 2, Score: 0.7},
	}

	sort.Sort(messages)

	// Should be sorted ascending by index
	expectedOrder := []int{1, 2, 3, 4}
	for i, expected := range expectedOrder {
		if messages[i].Index != expected {
			t.Errorf("Position %d: expected index %d, got %d", i, expected, messages[i].Index)
		}
	}
}

func TestNormalizeEmbedding(t *testing.T) {
	embedding := []float32{3.0, 4.0}

	normalized := NormalizeEmbedding(embedding)

	// ||[3,4]|| = 5, so normalized should be [0.6, 0.8]
	if math.Abs(float64(normalized[0])-0.6) > 1e-6 {
		t.Errorf("Expected normalized[0] = 0.6, got %f", normalized[0])
	}
	if math.Abs(float64(normalized[1])-0.8) > 1e-6 {
		t.Errorf("Expected normalized[1] = 0.8, got %f", normalized[1])
	}

	// Verify unit length
	var norm float64
	for _, v := range normalized {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1.0) > 1e-6 {
		t.Errorf("Normalized vector should have unit length, got %f", math.Sqrt(norm))
	}
}

func TestNormalizeEmbedding_ZeroVector(t *testing.T) {
	embedding := []float32{0.0, 0.0, 0.0}

	normalized := NormalizeEmbedding(embedding)

	// Should return original zero vector
	for i, v := range normalized {
		if v != 0.0 {
			t.Errorf("Zero vector should remain zero, got normalized[%d] = %f", i, v)
		}
	}
}

func TestBatchEmbeddingTexts(t *testing.T) {
	texts := []string{"a", "b", "c", "d", "e"}

	batches := BatchEmbeddingTexts(texts, 2)

	if len(batches) != 3 {
		t.Errorf("Expected 3 batches, got %d", len(batches))
	}

	if len(batches[0]) != 2 {
		t.Errorf("First batch should have 2 items, got %d", len(batches[0]))
	}
	if len(batches[1]) != 2 {
		t.Errorf("Second batch should have 2 items, got %d", len(batches[1]))
	}
	if len(batches[2]) != 1 {
		t.Errorf("Third batch should have 1 item, got %d", len(batches[2]))
	}
}

func TestBatchEmbeddingTexts_SingleBatch(t *testing.T) {
	texts := []string{"a", "b", "c"}

	batches := BatchEmbeddingTexts(texts, 10)

	if len(batches) != 1 {
		t.Errorf("Expected 1 batch, got %d", len(batches))
	}
	if len(batches[0]) != 3 {
		t.Errorf("Batch should have 3 items, got %d", len(batches[0]))
	}
}

func TestBatchEmbeddingTexts_ZeroBatchSize(t *testing.T) {
	texts := []string{"a", "b", "c"}

	batches := BatchEmbeddingTexts(texts, 0)

	// Should return single batch with all texts
	if len(batches) != 1 {
		t.Errorf("Expected 1 batch for zero batch size, got %d", len(batches))
	}
}

func TestBatchEmbeddingTexts_Empty(t *testing.T) {
	texts := []string{}

	batches := BatchEmbeddingTexts(texts, 2)

	if len(batches) != 0 {
		t.Errorf("Empty input should return no batches, got %d", len(batches))
	}
}

func TestScoredMessage_Fields(t *testing.T) {
	msg := types.Message{
		Role:    "user",
		Content: "Hello world",
	}

	scored := ScoredMessage{
		Index:       5,
		Message:     msg,
		Score:       0.85,
		IsProtected: true,
		TokenCount:  10,
	}

	if scored.Index != 5 {
		t.Errorf("Expected Index 5, got %d", scored.Index)
	}
	if scored.Score != 0.85 {
		t.Errorf("Expected Score 0.85, got %f", scored.Score)
	}
	if !scored.IsProtected {
		t.Error("Expected IsProtected true")
	}
	if scored.Message.Role != "user" {
		t.Errorf("Expected Role 'user', got %s", scored.Message.Role)
	}
}
