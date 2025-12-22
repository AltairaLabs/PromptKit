package stage

import (
	"math"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// CosineSimilarity computes the cosine similarity between two embedding vectors.
// Returns a value between -1.0 and 1.0, where:
//   - 1.0 means vectors are identical in direction
//   - 0.0 means vectors are orthogonal (unrelated)
//   - -1.0 means vectors are opposite
//
// For text embeddings, values typically range from 0.0 to 1.0, with higher
// values indicating greater semantic similarity.
//
// Returns 0.0 if vectors have different lengths, are empty, or have zero magnitude.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ScoredMessage pairs a message with its relevance score and metadata.
// Used during relevance-based truncation to track which messages to keep.
type ScoredMessage struct {
	// Index is the original position in the message slice
	Index int

	// Message is the actual message content
	Message types.Message

	// Score is the cosine similarity to the query (0.0 to 1.0)
	Score float64

	// IsProtected indicates if this message should always be kept
	// (e.g., recent messages or system messages)
	IsProtected bool

	// TokenCount is the estimated token count for this message
	TokenCount int
}

// ScoredMessages is a sortable slice of ScoredMessage.
type ScoredMessages []ScoredMessage

func (s ScoredMessages) Len() int           { return len(s) }
func (s ScoredMessages) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s ScoredMessages) Less(i, j int) bool { return s[i].Score > s[j].Score } // Descending by score

// ByOriginalIndex sorts ScoredMessages by their original index (ascending).
type ByOriginalIndex []ScoredMessage

func (s ByOriginalIndex) Len() int           { return len(s) }
func (s ByOriginalIndex) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s ByOriginalIndex) Less(i, j int) bool { return s[i].Index < s[j].Index }

// NormalizeEmbedding normalizes an embedding vector to unit length.
// This can improve similarity comparisons by ensuring all vectors
// have the same magnitude.
func NormalizeEmbedding(embedding []float32) []float32 {
	var norm float64
	for _, v := range embedding {
		norm += float64(v) * float64(v)
	}

	if norm == 0 {
		return embedding
	}

	norm = math.Sqrt(norm)
	result := make([]float32, len(embedding))
	for i, v := range embedding {
		result[i] = float32(float64(v) / norm)
	}
	return result
}

// BatchEmbeddingTexts splits texts into batches of the given size.
// Useful for respecting embedding provider batch limits.
func BatchEmbeddingTexts(texts []string, batchSize int) [][]string {
	if batchSize <= 0 {
		return [][]string{texts}
	}

	var batches [][]string
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, texts[i:end])
	}
	return batches
}
