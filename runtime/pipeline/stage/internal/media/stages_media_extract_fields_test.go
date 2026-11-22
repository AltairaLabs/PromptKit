package media

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A video part's duration, in whole seconds on the part, becomes a
// time.Duration on the extracted video.
func TestMediaContentToVideoData_DurationIsInSeconds(t *testing.T) {
	t.Parallel()
	data := base64.StdEncoding.EncodeToString([]byte("vid"))
	dur := 3
	got, err := mediaContentToVideoData(&types.MediaContent{MIMEType: "video/mp4", Data: &data, Duration: &dur}, false)
	require.NoError(t, err)
	assert.False(t, got.Timestamp.IsZero(), "the video is stamped when it is extracted")
	got.Timestamp = time.Time{}
	assert.Equal(t, &core.VideoData{MIMEType: "video/mp4", Data: []byte("vid"), Duration: 3 * time.Second}, got)
}

// An empty URL is no data source: the error says there is none, not that the
// URL needs loading.
func TestReadMediaBytes_EmptyURLIsNoDataSource(t *testing.T) {
	t.Parallel()
	empty := ""
	_, err := readMediaBytes(&types.MediaContent{MIMEType: "image/png", URL: &empty}, "image")
	assert.EqualError(t, err, "no data source available for image")
}
