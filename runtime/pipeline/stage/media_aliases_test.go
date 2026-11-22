package stage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/media"
)

// The facade's wrappers of internal/media's constructors: each default config
// is internal/media's, and each stage is built from the config it is given
// under its own name, so a stage.X caller sees no difference from before the
// move (#2876).

func TestFacade_MediaDefaultConfigs(t *testing.T) {
	assert.Equal(t, media.DefaultFrameRateLimitConfig(), DefaultFrameRateLimitConfig())
	assert.Equal(t, media.DefaultImagePreprocessConfig(), DefaultImagePreprocessConfig())
	assert.Equal(t, media.DefaultImageResizeStageConfig(), DefaultImageResizeStageConfig())
	assert.Equal(t, media.DefaultMediaComposeConfig(), DefaultMediaComposeConfig())
	assert.Equal(t, media.DefaultMediaConvertConfig(), DefaultMediaConvertConfig())
	assert.Equal(t, media.DefaultMediaExtractConfig(), DefaultMediaExtractConfig())
	assert.Equal(t, media.DefaultVideoToFramesConfig(), DefaultVideoToFramesConfig())
	assert.Equal(t, media.DefaultFramesToMessageConfig(), DefaultFramesToMessageConfig())
	assert.Equal(t, 30*time.Second, DefaultMediaComposeConfig().CompletionTimeout)
}

func TestFacade_MediaStagesKeepTheirConfigAndName(t *testing.T) {
	frameRate := FrameRateLimitConfig{TargetFPS: 4, DropStrategy: DropStrategyUniform}
	fr := NewFrameRateLimitStage(frameRate)
	assert.Equal(t, frameRate, fr.GetConfig())
	assert.Equal(t, "frame-rate-limit", fr.Name())

	preprocess := ImagePreprocessConfig{EnableResize: true}
	pp := NewImagePreprocessStage(preprocess)
	assert.Equal(t, preprocess, pp.GetConfig())
	assert.Equal(t, "image-preprocess", pp.Name())

	resize := ImageResizeStageConfig{MaxWidth: 7}
	rs := NewImageResizeStage(resize)
	assert.Equal(t, resize, rs.GetConfig())
	assert.Equal(t, "image-resize", rs.Name())

	compose := MediaComposeConfig{CompletionTimeout: time.Minute}
	mc := NewMediaComposeStage(compose)
	assert.Equal(t, compose, mc.GetConfig())
	assert.Equal(t, "media-compose", mc.Name())

	convert := MediaConvertConfig{TargetImageFormats: []string{"image/png"}}
	cv := NewMediaConvertStage(&convert)
	assert.Equal(t, convert, cv.GetConfig())
	assert.Equal(t, "media-convert", cv.Name())

	extract := MediaExtractConfig{ExtractImages: true}
	ex := NewMediaExtractStage(extract)
	assert.Equal(t, extract, ex.GetConfig())
	assert.Equal(t, "media-extract", ex.Name())

	toFrames := VideoToFramesConfig{MaxFrames: 3}
	vf := NewVideoToFramesStage(toFrames)
	assert.Equal(t, toFrames, vf.GetConfig())
	assert.Equal(t, "video-to-frames", vf.Name())

	toMessage := FramesToMessageConfig{MaxFramesPerMessage: 2}
	fm := NewFramesToMessageStage(toMessage)
	assert.Equal(t, toMessage, fm.GetConfig())
	assert.Equal(t, "frames-to-message", fm.Name())
}
