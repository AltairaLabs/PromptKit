package agentkb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEvalsReference_ContainsCatalog(t *testing.T) {
	out, err := GenerateEvalsReference()
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "# Evals & Assertions")
	assert.Contains(t, s, "text_toxicity")
	assert.Contains(t, s, "0..1 raw signal")
	// Alias table renders the canonical target.
	assert.Contains(t, s, "content_includes")
	assert.Contains(t, s, "`contains`")
	// The threshold trap is spelled out.
	assert.Contains(t, strings.ToLower(s), "threshold")
}

func TestEvalMeta_MatchesRegistry(t *testing.T) {
	meta, err := loadEvalMeta()
	require.NoError(t, err)

	registryIDs := map[string]bool{}
	for _, id := range canonicalEvalTypes() {
		registryIDs[id] = true
	}

	var missingFromMeta, orphanInMeta []string
	for id := range registryIDs {
		if _, ok := meta.Handlers[id]; !ok {
			missingFromMeta = append(missingFromMeta, id)
		}
	}
	for id := range meta.Handlers {
		if !registryIDs[id] {
			orphanInMeta = append(orphanInMeta, id)
		}
	}
	assert.Empty(t, missingFromMeta, "evalmeta.yaml missing descriptions for registered handlers: %v", missingFromMeta)
	assert.Empty(t, orphanInMeta, "evalmeta.yaml describes unknown handlers: %v", orphanInMeta)
}
