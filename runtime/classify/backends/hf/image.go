package hf

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// ClassifyImage submits image bytes to the HF image-classification
// endpoint. Same response shape as audio (flat [{label, score}] array).
func (c *Client) ClassifyImage(
	ctx context.Context, img []byte, opts classify.ImageOptions,
) ([]classify.LabelScore, error) {
	if len(img) == 0 {
		return nil, fmt.Errorf("hf: empty image bytes")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("hf: image classification requires opts.Model")
	}
	url, err := c.modelURL(opts.Model)
	if err != nil {
		return nil, err
	}
	contentType := opts.MIMEType
	if contentType == "" {
		contentType = defaultContentType
	}
	body, err := c.do(ctx, url, contentType, img)
	if err != nil {
		return nil, err
	}
	return decodeLabelScores(body)
}
