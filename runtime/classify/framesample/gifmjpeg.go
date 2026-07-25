package framesample

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"time"
)

// GIFMJPEGSampler is the shipped pure-Go Sampler. It decodes animated
// GIF (via image/gif, with frame composition) and MJPEG / raw-JPEG
// streams (via image/jpeg). Every other container returns
// ErrUnsupportedContainer.
//
// Safe for concurrent use: it holds no state.
type GIFMJPEGSampler struct{}

// NewGIFMJPEGSampler returns the default pure-Go sampler.
func NewGIFMJPEGSampler() *GIFMJPEGSampler { return &GIFMJPEGSampler{} }

// Compile-time check.
var _ Sampler = (*GIFMJPEGSampler)(nil)

// gifDelayUnit is the duration of one unit in GIF's Delay slice
// (hundredths of a second).
const gifDelayUnit = 10 * time.Millisecond

// assumedMJPEGInterval synthesizes timestamps for MJPEG frames, which
// carry no container timing. Only affects reported Frame.Timestamp; the
// classifier aggregates across frames regardless of timing.
const assumedMJPEGInterval = 100 * time.Millisecond

// Sample decodes video into sampled stills. It sniffs the container
// from opts.MIMEType, falling back to the leading bytes.
func (s *GIFMJPEGSampler) Sample(
	ctx context.Context, video []byte, opts SampleOptions,
) (SampleResult, error) {
	if len(video) == 0 {
		return SampleResult{}, fmt.Errorf("framesample: empty video payload")
	}
	switch detectContainer(video, opts.MIMEType) {
	case containerGIF:
		return sampleGIF(ctx, video, opts)
	case containerMJPEG:
		return sampleMJPEG(ctx, video, opts)
	case containerUnknown:
		return SampleResult{}, ErrUnsupportedContainer
	}
	return SampleResult{}, ErrUnsupportedContainer
}

// mimeJPEG / mimePNG label the still frames emitted by the sampler.
const (
	mimeJPEG = "image/jpeg"
	mimePNG  = "image/png"
)

type container int

const (
	containerUnknown container = iota
	containerGIF
	containerMJPEG
)

// detectContainer resolves the container from an explicit MIME hint,
// then from magic bytes. Unknown / codec-requiring containers resolve to
// containerUnknown so the caller returns ErrUnsupportedContainer.
func detectContainer(b []byte, mime string) container {
	switch mime {
	case "image/gif":
		return containerGIF
	case "video/x-motion-jpeg", mimeJPEG, "image/jpg":
		return containerMJPEG
	case "video/mp4", "video/quicktime", "video/webm", "video/mp2t":
		return containerUnknown
	}
	switch {
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return containerGIF
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8}):
		return containerMJPEG
	default:
		return containerUnknown
	}
}

// sampleGIF decodes an animated GIF, composes each frame onto a full
// canvas (honoring disposal), downselects by cadence / cap, and encodes
// the chosen frames as PNG.
func sampleGIF(ctx context.Context, video []byte, opts SampleOptions) (SampleResult, error) {
	g, err := gif.DecodeAll(bytes.NewReader(video))
	if err != nil {
		return SampleResult{}, fmt.Errorf("framesample: decode gif: %w", err)
	}
	if len(g.Image) == 0 {
		return SampleResult{}, fmt.Errorf("framesample: gif has no frames")
	}

	composed, timestamps := composeGIFFrames(g)
	picked := selectIndices(timestamps, opts)

	frames := make([]Frame, 0, len(picked))
	for _, idx := range picked {
		if err := ctx.Err(); err != nil {
			return SampleResult{}, err
		}
		var buf bytes.Buffer
		if encErr := png.Encode(&buf, composed[idx]); encErr != nil {
			return SampleResult{}, fmt.Errorf("framesample: encode frame %d: %w", idx, encErr)
		}
		frames = append(frames, Frame{
			Data:      buf.Bytes(),
			MIMEType:  mimePNG,
			Timestamp: timestamps[idx],
		})
	}
	return SampleResult{Frames: frames}, nil
}

// composeGIFFrames renders each GIF frame onto a persistent canvas so
// partial frames (the common optimization where a frame only carries the
// changed region) become complete stills. Returns the composed images
// and each frame's start timestamp.
func composeGIFFrames(g *gif.GIF) ([]*image.RGBA, []time.Duration) {
	bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	if bounds.Empty() {
		// Older encoders omit Config; fall back to the first frame's rect.
		bounds = g.Image[0].Bounds()
	}
	canvas := image.NewRGBA(bounds)
	composed := make([]*image.RGBA, len(g.Image))
	timestamps := make([]time.Duration, len(g.Image))

	var elapsed time.Duration
	for i, frame := range g.Image {
		var prev *image.RGBA
		disposal := disposalMethod(g, i)
		if disposal == gif.DisposalPrevious {
			prev = cloneRGBA(canvas)
		}

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		composed[i] = cloneRGBA(canvas)
		timestamps[i] = elapsed
		elapsed += time.Duration(delayAt(g, i)) * gifDelayUnit

		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if prev != nil {
				draw.Draw(canvas, canvas.Bounds(), prev, prev.Bounds().Min, draw.Src)
			}
		}
	}
	return composed, timestamps
}

// disposalMethod returns frame i's disposal code, tolerating a short or
// nil Disposal slice (older/simple GIFs).
func disposalMethod(g *gif.GIF, i int) byte {
	if i < len(g.Disposal) {
		return g.Disposal[i]
	}
	return gif.DisposalNone
}

// delayAt returns frame i's delay (in gifDelayUnit units), tolerating a
// short Delay slice.
func delayAt(g *gif.GIF, i int) int {
	if i < len(g.Delay) {
		return g.Delay[i]
	}
	return 0
}

// cloneRGBA returns a deep copy of src so a captured frame is not mutated
// by subsequent canvas draws.
func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// sampleMJPEG splits a JPEG / MJPEG stream into its constituent frames.
// MJPEG carries no container timing, so timestamps are synthesized at a
// fixed cadence and FramesPerSecond only influences the MaxFrames cap.
func sampleMJPEG(ctx context.Context, video []byte, opts SampleOptions) (SampleResult, error) {
	segments := splitJPEGFrames(video)
	if len(segments) == 0 {
		return SampleResult{}, fmt.Errorf("framesample: no jpeg frames found")
	}
	timestamps := make([]time.Duration, len(segments))
	for i := range segments {
		timestamps[i] = time.Duration(i) * assumedMJPEGInterval
	}
	picked := selectIndices(timestamps, opts)

	frames := make([]Frame, 0, len(picked))
	for _, idx := range picked {
		if err := ctx.Err(); err != nil {
			return SampleResult{}, err
		}
		// Validate the segment decodes before emitting it.
		if _, err := jpeg.DecodeConfig(bytes.NewReader(segments[idx])); err != nil {
			continue
		}
		frames = append(frames, Frame{
			Data:      segments[idx],
			MIMEType:  mimeJPEG,
			Timestamp: timestamps[idx],
		})
	}
	if len(frames) == 0 {
		return SampleResult{}, fmt.Errorf("framesample: no decodable jpeg frames")
	}
	return SampleResult{Frames: frames}, nil
}

// jpegSOI / jpegEOI are the JPEG start-of-image and end-of-image markers.
var (
	jpegSOI = []byte{0xFF, 0xD8}
	jpegEOI = []byte{0xFF, 0xD9}
)

// splitJPEGFrames extracts each JPEG frame from a concatenated MJPEG
// stream by scanning SOI…EOI marker pairs. A single JPEG yields one
// frame.
func splitJPEGFrames(b []byte) [][]byte {
	var frames [][]byte
	for {
		start := bytes.Index(b, jpegSOI)
		if start < 0 {
			break
		}
		end := bytes.Index(b[start+len(jpegSOI):], jpegEOI)
		if end < 0 {
			break
		}
		// end is relative to start+2; the frame includes the EOI marker.
		frameEnd := start + len(jpegSOI) + end + len(jpegEOI)
		frame := make([]byte, frameEnd-start)
		copy(frame, b[start:frameEnd])
		frames = append(frames, frame)
		b = b[frameEnd:]
	}
	return frames
}

// selectIndices picks frame indices to emit. When FramesPerSecond is set
// and the timeline has real duration, it samples evenly across the
// timeline at that cadence; otherwise it takes every frame. The result
// is then downselected evenly to at most the effective MaxFrames.
func selectIndices(timestamps []time.Duration, opts SampleOptions) []int {
	maxFrames := opts.MaxFrames
	if maxFrames <= 0 {
		maxFrames = DefaultMaxFrames
	}

	var idxs []int
	total := timestamps[len(timestamps)-1]
	if opts.FramesPerSecond > 0 && total > 0 {
		step := time.Duration(float64(time.Second) / opts.FramesPerSecond)
		if step <= 0 {
			step = gifDelayUnit
		}
		last := -1
		for tick := time.Duration(0); tick <= total; tick += step {
			i := frameActiveAt(timestamps, tick)
			if i != last {
				idxs = append(idxs, i)
				last = i
			}
		}
	} else {
		idxs = make([]int, len(timestamps))
		for i := range timestamps {
			idxs[i] = i
		}
	}
	return evenlyDownselect(idxs, maxFrames)
}

// frameActiveAt returns the index of the last frame whose start timestamp
// is <= t (the frame on screen at time t).
func frameActiveAt(timestamps []time.Duration, t time.Duration) int {
	active := 0
	for i, ts := range timestamps {
		if ts <= t {
			active = i
		} else {
			break
		}
	}
	return active
}

// evenlyDownselect returns at most maxFrames indices from idxs, spaced
// evenly (always keeping the first). Returns idxs unchanged when already
// within the cap.
func evenlyDownselect(idxs []int, maxFrames int) []int {
	if len(idxs) <= maxFrames {
		return idxs
	}
	out := make([]int, 0, maxFrames)
	stride := float64(len(idxs)) / float64(maxFrames)
	for k := range maxFrames {
		pos := int(float64(k) * stride)
		if pos >= len(idxs) {
			pos = len(idxs) - 1
		}
		out = append(out, idxs[pos])
	}
	return out
}
