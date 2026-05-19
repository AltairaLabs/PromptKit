package hf

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// ClassifyAudio submits raw audio bytes to the HF Inference API
// audio-classification endpoint for the configured model. Audio
// preparation (resampling to 16 kHz mono WAV, etc.) is the caller's
// responsibility — the handler upstream knows the source format from
// message metadata and converts before calling.
//
// HF returns `[{"label": "...", "score": ...}, ...]` sorted by
// descending score; we pass that order through.
func (c *Client) ClassifyAudio(
	ctx context.Context, audio []byte, opts classify.AudioOptions,
) ([]classify.LabelScore, error) {
	if len(audio) == 0 {
		return nil, fmt.Errorf("hf: empty audio bytes")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("hf: audio classification requires opts.Model")
	}
	url, err := c.modelURL(opts.Model)
	if err != nil {
		return nil, err
	}
	contentType := opts.MIMEType
	if contentType == "" {
		// HF accepts most audio content types; raw octet-stream is
		// the safest default when the caller didn't tag it. Models
		// that need explicit format selection will surface a 400.
		contentType = defaultContentType
	}
	body, err := c.do(ctx, url, contentType, audio)
	if err != nil {
		return nil, err
	}
	return decodeLabelScores(body)
}

// decodeLabelScores parses HF's standard label/score array response.
// Used by audio, text, image classifier paths — same shape.
func decodeLabelScores(body []byte) ([]classify.LabelScore, error) {
	var raw []struct {
		Label string  `json:"label"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("hf: decode label-score array: %w (body: %s)", err, truncateBody(body))
	}
	out := make([]classify.LabelScore, len(raw))
	for i, r := range raw {
		out[i] = classify.LabelScore{Label: r.Label, Score: r.Score}
	}
	return out, nil
}
