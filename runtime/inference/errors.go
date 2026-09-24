package inference

import "errors"

var (
	// ErrModelLoading is returned when the provider reports the model is
	// still loading after retries are exhausted.
	ErrModelLoading = errors.New("inference: model still loading after retries")
	// ErrModelNotSupported is returned when the provider rejects the
	// configured model as unsupported by the inference path in use.
	ErrModelNotSupported = errors.New("inference: model not supported by the configured inference path")
	// ErrLabelsRequired is returned by providers that need candidate labels
	// (Request.Labels) but received none.
	ErrLabelsRequired = errors.New("inference: this provider needs candidate labels")
)
