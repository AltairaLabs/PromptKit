// Package framesample decodes video bytes into evenly-spaced still
// frames (plus an optional audio track) so a decomposing video
// classifier can route them through image and audio classifiers.
//
// The shipped implementation is pure Go — no CGO and no subprocess to
// system tools (ffmpeg et al.) — so it runs on a `FROM scratch`
// container. That constraint bounds the supported containers: animated
// GIF (fully native via image/gif) and MJPEG / raw-JPEG streams.
// Containers that need a real codec (mp4/H.264, WebM/VP9, MPEG-TS) are
// reported with ErrUnsupportedContainer rather than decoded — a caller
// running in a richer environment can supply an ffmpeg-backed Sampler
// behind this same interface without touching the classifier.
package framesample

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupportedContainer is returned when the input container cannot be
// decoded by the pure-Go sampler. Eval handlers map it to a skipped
// result — it is an environmental limitation, not a failure the user
// misconfigured.
var ErrUnsupportedContainer = errors.New("framesample: unsupported container for pure-Go decoding")

// Sampler turns video bytes into sampled still frames and, optionally,
// the raw audio track. Implementations must be safe for concurrent use.
type Sampler interface {
	Sample(ctx context.Context, video []byte, opts SampleOptions) (SampleResult, error)
}

// SampleOptions steers a Sample call. Zero values select sensible
// defaults; a sampler ignores fields it cannot honor.
type SampleOptions struct {
	// MIMEType is the container hint (e.g. "image/gif",
	// "video/x-motion-jpeg"). Empty means "sniff from the bytes".
	MIMEType string

	// FramesPerSecond is the target sampling cadence. Frames are picked
	// evenly across the clip's timeline at this rate. 0 selects every
	// decoded frame (still bounded by MaxFrames).
	FramesPerSecond float64

	// MaxFrames caps how many frames Sample returns, so a long clip
	// cannot fan out into an unbounded number of image classifications.
	// 0 uses the sampler default (DefaultMaxFrames).
	MaxFrames int

	// ExtractAudio requests the audio track when the container carries a
	// decodable one. GIF has no audio; MJPEG has none — for the shipped
	// pure-Go sampler this is a forward-compatibility seam that yields a
	// nil AudioPCM today.
	ExtractAudio bool
}

// SampleResult is the decoded output of a Sample call.
type SampleResult struct {
	// Frames are the sampled stills in timeline order, each encoded as a
	// standalone image (PNG for GIF-sourced frames, the original JPEG
	// bytes for MJPEG-sourced frames).
	Frames []Frame

	// AudioPCM is the raw audio track when ExtractAudio was set and the
	// container carried a decodable one; nil otherwise.
	AudioPCM []byte

	// AudioMIME describes AudioPCM (e.g. "audio/wav") when present.
	AudioMIME string
}

// Frame is a single sampled still image plus its position in the clip.
type Frame struct {
	// Data is the encoded still image.
	Data []byte

	// MIMEType is Data's encoding (e.g. "image/png", "image/jpeg").
	MIMEType string

	// Timestamp is the frame's offset from the start of the clip.
	Timestamp time.Duration
}

// DefaultMaxFrames bounds fan-out when SampleOptions.MaxFrames is 0.
// Keeps a long animation from spawning hundreds of image
// classifications; max/mean/vote aggregation is robust to sampling.
const DefaultMaxFrames = 16
