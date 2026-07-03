package stage

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestPNGData creates a valid PNG image with the given dimensions.
func createTestPNGData(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// writeFrameFile writes data to <dir>/<name>.
func writeFrameFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}

// drainFrames reads all elements from ch into a slice.
func drainFrames(ch chan StreamElement) []StreamElement {
	var out []StreamElement
	for {
		select {
		case elem := <-ch:
			out = append(out, elem)
		default:
			return out
		}
	}
}

func TestReadExtractedFrames_HappyPath(t *testing.T) {
	dir := t.TempDir()
	// Two JPEG frames (32x24, 40x30) and one PNG frame (16x16).
	writeFrameFile(t, dir, "frame_0002.jpg", createTestFrameData(40, 30))
	writeFrameFile(t, dir, "frame_0001.jpg", createTestFrameData(32, 24))
	writeFrameFile(t, dir, "frame_0003.png", createTestPNGData(16, 16))
	// Non-frame files that must be ignored.
	writeFrameFile(t, dir, "input.mp4", []byte("not a frame"))
	writeFrameFile(t, dir, "frame_notes.txt", []byte("skip me"))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "frame_subdir"), 0o750))

	video := &VideoData{Data: []byte("orig"), MIMEType: "video/mp4"}
	extractInfo := &MediaExtractInfo{MessageID: "msg-1", PartIndex: 2, MediaType: "video"}
	elem := &StreamElement{
		Sequence: 7,
		Source:   "cam",
		Video:    video,
	}
	elem.Meta.MediaExtract = extractInfo

	stg := NewVideoToFramesStage(DefaultVideoToFramesConfig())
	out := make(chan StreamElement, 10)
	n, err := stg.readExtractedFrames(context.Background(), dir, "vid-42", elem, out)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	frames := drainFrames(out)
	require.Len(t, frames, 3)

	// Ordering follows sorted filenames: frame_0001.jpg, frame_0002.jpg, frame_0003.png.
	assert.Equal(t, 32, frames[0].Image.Width)
	assert.Equal(t, 24, frames[0].Image.Height)
	assert.Equal(t, "image/jpeg", frames[0].Image.MIMEType)
	assert.Equal(t, "jpeg", frames[0].Image.Format)

	assert.Equal(t, 40, frames[1].Image.Width)
	assert.Equal(t, 30, frames[1].Image.Height)

	// Third frame is a PNG.
	assert.Equal(t, "image/png", frames[2].Image.MIMEType)
	assert.Equal(t, "png", frames[2].Image.Format)
	assert.Equal(t, 16, frames[2].Image.Width)

	for i, f := range frames {
		require.NotNil(t, f.Meta.VideoFrames, "frame %d missing VideoFrames meta", i)
		assert.Equal(t, "vid-42", f.Meta.VideoFrames.VideoID)
		assert.Equal(t, i, f.Meta.VideoFrames.FrameIndex)
		assert.Equal(t, 3, f.Meta.VideoFrames.TotalFrames)
		assert.Same(t, video, f.Meta.VideoFrames.OriginalVideo)
		assert.Equal(t, elem.Sequence, f.Sequence)
		assert.Equal(t, elem.Source, f.Source)

		// MediaExtract must be propagated as an independent copy.
		require.NotNil(t, f.Meta.MediaExtract, "frame %d missing MediaExtract", i)
		assert.Equal(t, "msg-1", f.Meta.MediaExtract.MessageID)
		assert.NotSame(t, extractInfo, f.Meta.MediaExtract, "MediaExtract should be copied, not aliased")
	}
}

func TestReadExtractedFrames_NoMediaExtract(t *testing.T) {
	dir := t.TempDir()
	writeFrameFile(t, dir, "frame_0001.jpg", createTestFrameData(8, 8))

	elem := &StreamElement{Video: &VideoData{Data: []byte("v")}}
	stg := NewVideoToFramesStage(DefaultVideoToFramesConfig())
	out := make(chan StreamElement, 4)
	n, err := stg.readExtractedFrames(context.Background(), dir, "vid", elem, out)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	frames := drainFrames(out)
	require.Len(t, frames, 1)
	assert.Nil(t, frames[0].Meta.MediaExtract)
}

func TestReadExtractedFrames_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Only non-frame content present.
	writeFrameFile(t, dir, "readme.txt", []byte("hi"))

	stg := NewVideoToFramesStage(DefaultVideoToFramesConfig())
	out := make(chan StreamElement, 2)
	n, err := stg.readExtractedFrames(context.Background(), dir, "vid", &StreamElement{}, out)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, drainFrames(out))
}

func TestReadExtractedFrames_UnreadableDir(t *testing.T) {
	stg := NewVideoToFramesStage(DefaultVideoToFramesConfig())
	out := make(chan StreamElement, 1)
	n, err := stg.readExtractedFrames(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), "vid", &StreamElement{}, out)
	require.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestReadExtractedFrames_UndecodableFrameKeepsZeroDims(t *testing.T) {
	dir := t.TempDir()
	// A .jpg file that is not a valid image: image.Decode fails, dims stay 0,
	// but the frame is still emitted.
	writeFrameFile(t, dir, "frame_0001.jpg", []byte("this is not a jpeg"))

	stg := NewVideoToFramesStage(DefaultVideoToFramesConfig())
	out := make(chan StreamElement, 2)
	n, err := stg.readExtractedFrames(context.Background(), dir, "vid", &StreamElement{}, out)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	frames := drainFrames(out)
	require.Len(t, frames, 1)
	assert.Equal(t, 0, frames[0].Image.Width)
	assert.Equal(t, 0, frames[0].Image.Height)
}

func TestReadExtractedFrames_ContextCanceledDuringEmit(t *testing.T) {
	dir := t.TempDir()
	writeFrameFile(t, dir, "frame_0001.jpg", createTestFrameData(8, 8))
	writeFrameFile(t, dir, "frame_0002.jpg", createTestFrameData(8, 8))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before emitting

	stg := NewVideoToFramesStage(DefaultVideoToFramesConfig())
	out := make(chan StreamElement) // unbuffered so the send blocks
	n, err := stg.readExtractedFrames(ctx, dir, "vid", &StreamElement{}, out)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, n)
}
