package classify

import "context"

// Embedder turns text inputs into dense vectors. Co-located with the
// classifiers because it shares the same shape (task interface +
// multiple backends + registry lookup) and because moving embeddings
// here breaks the historical dependency between
// runtime/evals/handlers/cosine_similarity.go and concrete provider
// packages.
//
// The current cosine_similarity handler reads pre-computed embeddings
// from EvalContext.Metadata — it doesn't call an Embedder directly.
// Migrating to this interface is queued (see phase 2 of the inference
// abstraction proposal); the interface lives here from day one so the
// migration doesn't introduce a new package later.
type Embedder interface {
	Embed(ctx context.Context, inputs []string, opts EmbedOptions) ([][]float32, error)
}
