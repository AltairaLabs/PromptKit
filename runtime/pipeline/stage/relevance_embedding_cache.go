package stage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// embeddingKey identifies one embedded text: the embedding provider and model
// that produced the vector, and a hash of the exact text that was embedded.
// It never depends on a message's position, so an edited message misses and a
// reordered one still hits.
type embeddingKey struct {
	identity string
	sum      [sha256.Size]byte
}

// embeddingCache holds the vectors relevance truncation computed on its most
// recent call, for RelevanceConfig.CacheEmbeddings.
//
// It lives on the ContextBuilderStage, so it lasts as long as the stage does:
// one conversation in the SDK, which builds its pipeline once per
// Conversation. After each successful call it keeps only the vectors that call
// used, so it never holds more than one call's texts, and a message that has
// left the input (dropped from a hot window, edited, or a replaced query) is
// released on the next call. Safe for concurrent use.
type embeddingCache struct {
	mu      sync.Mutex
	vectors map[embeddingKey][]float32
}

// embeddingIdentity names what produced a vector: the provider's ID plus its
// model, when the provider reports one.
func embeddingIdentity(p providers.EmbeddingProvider) string {
	identity := p.ID()
	if m, ok := p.(interface{ Model() string }); ok {
		identity += "\x00" + m.Model()
	}
	return identity
}

// embedTexts returns one vector per text, in order. Without caching it makes a
// single Embed call for every text. With caching it embeds only the texts
// whose vectors the previous call did not already hold, then keeps this
// call's vectors for the next one.
func (s *ContextBuilderStage) embedTexts(
	ctx context.Context,
	cfg *RelevanceConfig,
	texts []string,
) ([][]float32, error) {
	if !cfg.CacheEmbeddings {
		return embedAll(ctx, cfg.EmbeddingProvider, texts)
	}
	return s.embedCache.embed(ctx, cfg.EmbeddingProvider, texts)
}

// embedAll embeds texts in one call and checks one vector came back per text.
func embedAll(ctx context.Context, p providers.EmbeddingProvider, texts []string) ([][]float32, error) {
	resp, err := p.Embed(ctx, providers.EmbeddingRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("embedding generation failed: %w", err)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: expected %d, got %d",
			len(texts), len(resp.Embeddings))
	}
	return resp.Embeddings, nil
}

// embed returns one vector per text, embedding only the cache misses (each
// distinct missing text once), and replaces the cache with this call's
// vectors. On an embedding error the cache is left as it was.
func (c *embeddingCache) embed(
	ctx context.Context,
	p providers.EmbeddingProvider,
	texts []string,
) ([][]float32, error) {
	identity := embeddingIdentity(p)
	keys := make([]embeddingKey, len(texts))
	for i, text := range texts {
		keys[i] = embeddingKey{identity: identity, sum: sha256.Sum256([]byte(text))}
	}

	found, missing, missingKeys := c.lookup(keys, texts)

	if len(missing) > 0 {
		vectors, err := embedAll(ctx, p, missing)
		if err != nil {
			return nil, err
		}
		for i, key := range missingKeys {
			found[key] = vectors[i]
		}
	}

	out := make([][]float32, len(texts))
	for i, key := range keys {
		out[i] = found[key]
	}

	c.mu.Lock()
	c.vectors = found
	c.mu.Unlock()
	return out, nil
}

// lookup splits keys into the vectors already cached and the distinct texts
// still to embed. found is a fresh map, safe to fill without the lock.
func (c *embeddingCache) lookup(
	keys []embeddingKey,
	texts []string,
) (found map[embeddingKey][]float32, missing []string, missingKeys []embeddingKey) {
	found = make(map[embeddingKey][]float32, len(keys))
	pending := make(map[embeddingKey]bool)

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, key := range keys {
		if _, ok := found[key]; ok || pending[key] {
			continue
		}
		if vec, ok := c.vectors[key]; ok {
			found[key] = vec
			continue
		}
		pending[key] = true
		missing = append(missing, texts[i])
		missingKeys = append(missingKeys, key)
	}
	return found, missing, missingKeys
}
