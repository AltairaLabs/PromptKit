package classify

import "context"

// ImageClassifier classifies a single still image. Visual content
// moderation (NSFW, violence, brand safety), scene tagging, object
// recognition all go through this. No streaming sibling — a still
// image is a single inference.
type ImageClassifier interface {
	ClassifyImage(ctx context.Context, img []byte, opts ImageOptions) ([]LabelScore, error)
}
