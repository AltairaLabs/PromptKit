package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime (duplex) session does not run tool loading: its tools follow its
// workflow state's skills filter in stable mode as in rebuild mode. A state
// that admits no skill (skills: none, applied here as a transition applies
// it) withholds skill__activate from the session.
func TestToolLoadingStable_RealtimeSessionKeepsTheStateFilteredIndex(t *testing.T) {
	for _, mode := range []ToolLoadingMode{ToolLoadingRebuild, ToolLoadingStable} {
		spy := &sessionConfigSpy{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")}
		conv, err := OpenDuplex(writeSkillIndexPack(t, false), "pa", WithProvider(spy), WithSkipSchemaValidation(),
			WithToolLoading(ToolLoading{Mode: mode}),
			WithStreamingConfig(&providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000, Channels: 1, BitDepth: 16,
				Encoding: "pcm_linear16"}}))
		require.NoError(t, err)
		conv.applySkillFilter("locked", "none")

		require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{Content: "hello"}))
		require.Eventually(t, func() bool { return spy.first() != nil }, 5*time.Second, 10*time.Millisecond)

		var names []string
		for _, d := range spy.first().Tools {
			names = append(names, d.Name)
		}
		assert.NotContains(t, names, skills.SkillActivateTool, "%s: the state admits no skill", mode)
		assert.Contains(t, names, skills.SkillDeactivateTool, mode)
		_ = conv.Close()
	}
}
