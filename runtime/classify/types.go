// Package classify defines task-oriented inference interfaces for
// non-LLM workloads — audio / text / image / video classifiers and
// embedders. Backends (HuggingFace Inference API, ONNX, Replicate, …)
// implement one or more of these; eval handlers depend on the task
// interface, never on the backend.
//
// Parallel to runtime/tts and runtime/stt: each is a task interface
// with multiple backends behind it. runtime/providers handles chat
// completion (Predict); classify covers the rest.
package classify

// LabelScore pairs a classifier label with a confidence score in
// [0, 1]. Backends return slices of these sorted by descending score
// where the model supports ranking; the caller treats the order as
// hint only and looks up by Label when checking expected values.
type LabelScore struct {
	Label string
	Score float64
}

// AudioOptions carries per-call knobs for audio classification.
// Backends ignore fields they don't support; consumers set what
// matters for their target model.
type AudioOptions struct {
	// Model is the backend-specific model identifier (e.g.
	// "superb/wav2vec2-base-superb-er" on HuggingFace).
	Model string

	// Language is an optional ISO-639-1 hint (e.g. "en"). Only used
	// by backends that route by language.
	Language string

	// SampleRate is the source audio sample rate in Hz. Backends
	// that can resample internally use this to drive it. The
	// shipped HuggingFace backend does NOT resample — handlers
	// are expected to deliver audio at the rate the target model
	// wants (typically 16 kHz mono for SER models). The field is
	// kept on the options struct so future backends (ONNX, Hume,
	// etc.) that do resample have a place to read the source rate
	// from without growing a per-backend option type.
	SampleRate int

	// MIMEType is the audio container / codec hint (e.g. "audio/wav",
	// "audio/L16"). Backends that need explicit format selection use
	// this; others ignore.
	MIMEType string
}

// TextOptions carries per-call knobs for text classification.
type TextOptions struct {
	// Model is the backend-specific model identifier (e.g.
	// "unitary/toxic-bert" on HuggingFace).
	Model string

	// Language is an optional ISO-639-1 hint.
	Language string

	// MultiLabel asks the backend to return scores for every label
	// rather than a single best one. Useful for toxicity panels
	// (toxic, severe_toxic, obscene, …) where multiple may apply.
	MultiLabel bool
}

// ImageOptions carries per-call knobs for image classification.
type ImageOptions struct {
	// Model is the backend-specific model identifier (e.g.
	// "Falconsai/nsfw_image_detection" on HuggingFace).
	Model string

	// MIMEType is the image format hint (e.g. "image/png", "image/jpeg").
	MIMEType string
}

// VideoOptions carries per-call knobs for video classification.
type VideoOptions struct {
	// Model is the backend-specific model identifier.
	Model string

	// MIMEType is the video container hint (e.g. "video/mp4").
	MIMEType string

	// FrameSampleRate is the per-second frame extraction rate used
	// by decomposing backends (audio + sampled image classifier).
	// Whole-clip backends ignore this. 0 = backend default.
	FrameSampleRate float64

	// ExtractAudio toggles whether decomposing backends route the
	// audio track through their AudioClassifier as well as their
	// ImageClassifier. Default false.
	ExtractAudio bool
}

// EmbedOptions carries per-call knobs for text embedding.
type EmbedOptions struct {
	// Model is the backend-specific model identifier (e.g.
	// "voyage-3", "text-embedding-3-large").
	Model string

	// Dimensions optionally requests truncation to a specific
	// dimensionality. Backend ignores if not supported.
	Dimensions int

	// InputType disambiguates query vs document embedding for
	// asymmetric models (e.g. Voyage's `query` / `document`).
	// Empty = backend default.
	InputType string
}
