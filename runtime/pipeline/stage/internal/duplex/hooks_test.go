package duplex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// A provider's cues play on the bank of the cue stage NewDuplexCueStage
// creates for it, and on none once DetachCues unwires it.
func TestCuesOf_IsTheBankOfTheWiredCueStage(t *testing.T) {
	s := NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
	assert.Nil(t, CuesOf(s), "no cue stage plays a new provider's cues")

	cs, ok := NewDuplexCueStage(s, cue.CueOptions{}).(*cue.CueStage)
	require.True(t, ok, "NewDuplexCueStage returns a cue stage")
	assert.Same(t, cue.BankOf(cs), CuesOf(s))

	DetachCues(s)
	assert.Nil(t, CuesOf(s), "a detached provider plays no cues")
}

// ReorderPlaceholder is the caller text a reorder stage was created with.
func TestReorderPlaceholder_IsTheConfiguredText(t *testing.T) {
	assert.Equal(t, "[inaudible]", ReorderPlaceholder(NewTranscriptReorderStage("[inaudible]")))
	assert.Equal(t, "", ReorderPlaceholder(NewTranscriptReorderStageWithTimeout("", time.Second)))
}
