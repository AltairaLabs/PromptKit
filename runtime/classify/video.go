package classify

import "context"

// VideoClassifier classifies a video clip. Two physical shapes:
//
//   - Whole-clip backends (e.g. HuggingFace Video Classification API)
//     accept the full clip and return labels for it.
//   - Decomposing backends extract the audio track and frame-sample
//     stills, route those through AudioClassifier + ImageClassifier,
//     and aggregate the per-modality scores. The interface is the
//     same; backends choose the implementation.
//
// VideoOptions.FrameSampleRate and .ExtractAudio steer decomposing
// backends; whole-clip backends ignore them.
type VideoClassifier interface {
	ClassifyVideo(ctx context.Context, video []byte, opts VideoOptions) ([]LabelScore, error)
}

// StreamingVideoClassifier is the optional capability for live
// video classification (mostly a future concern — agentic-UI replay,
// security camera streams). No MVP backend implements it; the
// interface stays in the package so the synchronous one doesn't
// break when a streaming consumer arrives.
type StreamingVideoClassifier interface {
	VideoClassifier
	ClassifyVideoStream(
		ctx context.Context,
		// chunks is a stream of (frame, audio-segment) tuples
		// emitted by the source pipeline. Specific format TBD —
		// not implementing this MVP, just reserving the slot.
		chunks <-chan struct{},
		opts VideoOptions,
	) (<-chan LabelScoreEvent, error)
}
