package media

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
)

// notAnImage is ten bytes no decoder accepts. An image that skips resizing
// comes back with them untouched; one the stage tries to resize comes back
// with a "resize failed" error. So the outcome says which way the stage
// decided, from the declared dimensions and size alone.
var notAnImage = []byte("not-an-img")

// Which images ImageResizeStage decodes and which it passes through, by the
// limits configured and the width, height and size the image declares.
func TestImageResizeStage_DecidesFromDeclaredDimensionsAndSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		maxW, maxH        int
		maxBytes          int64
		width, height     int
		wantResizeAttempt bool
	}{
		{name: "within both limits, at the edge", maxW: 100, maxH: 100, width: 100, height: 100},
		{name: "within both limits", maxW: 100, maxH: 100, width: 1, height: 1},
		{name: "one pixel too wide", maxW: 100, maxH: 100, width: 101, height: 50, wantResizeAttempt: true},
		{name: "one pixel too tall", maxW: 100, maxH: 100, width: 50, height: 101, wantResizeAttempt: true},
		{name: "unknown width", maxW: 100, maxH: 100, width: 0, height: 50, wantResizeAttempt: true},
		{name: "unknown height", maxW: 100, maxH: 100, width: 50, height: 0, wantResizeAttempt: true},
		{name: "width limit only, unknown dimensions", maxW: 100, wantResizeAttempt: true},
		{name: "height limit only, unknown dimensions", maxH: 100, wantResizeAttempt: true},
		{name: "width limit only, too wide", maxW: 100, width: 150, height: 50, wantResizeAttempt: true},
		{name: "height limit only, too tall", maxH: 100, width: 50, height: 150, wantResizeAttempt: true},
		{name: "size limit only, unknown dimensions, small", maxBytes: 1000},
		{name: "size limit only, known dimensions, small", maxBytes: 1000, width: 50, height: 50},
		{name: "no limits", width: 50, height: 50},
		{name: "no limits, unknown dimensions"},
		{name: "size limit, exactly at it", maxBytes: int64(len(notAnImage)), width: 5, height: 5},
		{name: "size limit, one byte over", maxBytes: int64(len(notAnImage)) - 1, width: 5, height: 5,
			wantResizeAttempt: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := ImageResizeStageConfig{MaxWidth: tc.maxW, MaxHeight: tc.maxH, MaxSizeBytes: tc.maxBytes,
				Quality: 80, PreserveAspectRatio: true, SkipIfSmaller: true}
			data := append([]byte(nil), notAnImage...)
			got := runResizeStage(t, config, &core.ImageData{Data: data, MIMEType: "image/jpeg",
				Width: tc.width, Height: tc.height})

			if tc.wantResizeAttempt {
				require.Error(t, got.Error)
				assert.Contains(t, got.Error.Error(), "resize failed:")
				return
			}
			assert.NoError(t, got.Error)
			assert.Equal(t, notAnImage, got.Image.Data)
		})
	}
}

// An image element with no data is forwarded as it is: there is nothing to
// resize, so no error either.
func TestImageResizeStage_EmptyImageIsForwarded(t *testing.T) {
	t.Parallel()
	config := ImageResizeStageConfig{MaxWidth: 100, MaxHeight: 100, Quality: 80, SkipIfSmaller: true}
	got := runResizeStage(t, config, &core.ImageData{MIMEType: "image/jpeg"})
	assert.NoError(t, got.Error)
	assert.Equal(t, &core.ImageData{MIMEType: "image/jpeg"}, got.Image)
}
