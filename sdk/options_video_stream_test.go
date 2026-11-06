package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// TestWithStreamingVideo_ResizeReachesPipelineConfig guards the wiring: the
// resize fields of VideoStreamConfig must arrive in the pipeline config, or
// frames reach the provider at full size while the caller believes they were
// scaled down.
func TestWithStreamingVideo_ResizeReachesPipelineConfig(t *testing.T) {
	conv := &Conversation{
		config: configFromOptions(t, WithStreamingVideo(&VideoStreamConfig{
			TargetFPS: 2, MaxWidth: 320, MaxHeight: 240, Quality: 60, EnableResize: true,
		})),
		toolRegistry: tools.NewRegistry(),
		prompt:       &pack.Prompt{},
	}

	cfg := conv.buildPipelineConfig(nil, "conv-1", nil, nil)

	require.NotNil(t, cfg.VideoStreamConfig)
	assert.Equal(t, 2.0, cfg.VideoStreamConfig.TargetFPS)
	require.NotNil(t, cfg.VideoFrameResize, "VideoStreamConfig resize fields did not reach the pipeline config")
	assert.Equal(t, 320, cfg.VideoFrameResize.MaxWidth)
	assert.Equal(t, 240, cfg.VideoFrameResize.MaxHeight)
	assert.Equal(t, 60, cfg.VideoFrameResize.Quality)
}
