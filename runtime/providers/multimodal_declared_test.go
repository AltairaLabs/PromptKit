package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyDeclaredModalities(t *testing.T) {
	base := MultimodalCapabilities{SupportsImages: true, SupportsDocuments: true}

	t.Run("nil set leaves caps unchanged", func(t *testing.T) {
		assert.Equal(t, base, ApplyDeclaredModalities(base, nil))
	})

	t.Run("declared set is authoritative for every modality", func(t *testing.T) {
		got := ApplyDeclaredModalities(base, CapabilitySet([]string{CapabilityAudio, CapabilityVideo}))
		assert.False(t, got.SupportsImages)
		assert.True(t, got.SupportsAudio)
		assert.True(t, got.SupportsVideo)
		assert.False(t, got.SupportsDocuments)
	})
}
