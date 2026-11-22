package media

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// preprocessOne runs elem through an ImagePreprocessStage with the default
// config and returns what it emitted.
func preprocessOne(t *testing.T, elem core.StreamElement) core.StreamElement {
	t.Helper()
	input := make(chan core.StreamElement, 1)
	output := make(chan core.StreamElement, 1)
	input <- elem
	close(input)
	require.NoError(t, NewImagePreprocessStage(DefaultImagePreprocessConfig()).Process(context.Background(), input, output))
	return <-output
}

// A standalone image that cannot be decoded is forwarded with the error that
// says so.
func TestImagePreprocessStage_UndecodableImageElementCarriesTheError(t *testing.T) {
	t.Parallel()
	got := preprocessOne(t, core.StreamElement{Image: &core.ImageData{MIMEType: "image/jpeg", Data: []byte("not an image")}})
	require.Error(t, got.Error)
	assert.Contains(t, got.Error.Error(), "failed to resize image data:")
}

// An image part whose file path is empty has no data source: it is left as it
// is, without an error.
func TestImagePreprocessStage_EmptyFilePathIsNoDataSource(t *testing.T) {
	t.Parallel()
	empty := ""
	part := types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/jpeg", FilePath: &empty}}
	got := preprocessOne(t, core.StreamElement{Message: &types.Message{Role: "user", Parts: []types.ContentPart{part}}})
	assert.NoError(t, got.Error)
	assert.Equal(t, &types.MediaContent{MIMEType: "image/jpeg", FilePath: &empty}, got.Message.Parts[0].Media)
}
