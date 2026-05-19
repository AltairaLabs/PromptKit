package hf

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// ClassifyText submits text to the HF text-classification endpoint.
// HF returns nested arrays — `[[{label, score}, ...]]` — one inner
// array per input. We send one input so the outer array has length 1;
// flattening returns the inner labels directly.
func (c *Client) ClassifyText(
	ctx context.Context, text string, opts classify.TextOptions,
) ([]classify.LabelScore, error) {
	if text == "" {
		return nil, fmt.Errorf("hf: empty text")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("hf: text classification requires opts.Model")
	}
	url, err := c.modelURL(opts.Model)
	if err != nil {
		return nil, err
	}

	type textRequest struct {
		Inputs     string         `json:"inputs"`
		Parameters map[string]any `json:"parameters,omitempty"`
	}
	req := textRequest{Inputs: text}
	if opts.MultiLabel {
		req.Parameters = map[string]any{"return_all_scores": true}
	}
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("hf: encode text request: %w", err)
	}

	body, err := c.do(ctx, url, "application/json", reqBody)
	if err != nil {
		return nil, err
	}

	// HF returns either [[{...}]] (multi-label / batch shape) or
	// [{...}] (single label, no return_all_scores). Try the nested
	// shape first; fall back to flat.
	var nested [][]struct {
		Label string  `json:"label"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal(body, &nested); err == nil && len(nested) > 0 {
		out := make([]classify.LabelScore, len(nested[0]))
		for i, r := range nested[0] {
			out[i] = classify.LabelScore{Label: r.Label, Score: r.Score}
		}
		return out, nil
	}
	return decodeLabelScores(body)
}
