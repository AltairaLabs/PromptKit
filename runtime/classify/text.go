package classify

import "context"

// TextClassifier classifies a single text string. Toxicity, sentiment,
// emotion-from-text, intent, language detection — anything that takes
// text in and returns labeled scores.
//
// Text inputs aren't temporally extended, so there's no streaming
// sibling. Batch text classification (many strings at once) goes
// through repeated calls — backends that support batching internally
// can pool requests.
type TextClassifier interface {
	ClassifyText(ctx context.Context, text string, opts TextOptions) ([]LabelScore, error)
}
