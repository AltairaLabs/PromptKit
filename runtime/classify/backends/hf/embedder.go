package hf

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// Embed turns text inputs into dense float32 vectors. HF's feature-
// extraction endpoint returns one vector per input — either as a
// flat float array for a single input or a nested array for a batch.
// We always send a list and unmarshal the batch shape so callers see
// a uniform `[][]float32`.
//
// Some HF embedding models (sentence-transformers) return per-token
// embeddings as `[[[float]]]` (batch × tokens × dims) and require the
// caller to mean-pool. The MVP only supports models that already
// return one vector per input; per-token responses surface as a
// decode error.
func (c *Client) Embed(
	ctx context.Context, inputs []string, opts classify.EmbedOptions,
) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("hf: empty inputs")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("hf: embedding requires opts.Model")
	}
	url, err := c.modelURL(opts.Model)
	if err != nil {
		return nil, err
	}

	type embedRequest struct {
		Inputs []string `json:"inputs"`
	}
	reqBody, err := json.Marshal(embedRequest{Inputs: inputs})
	if err != nil {
		return nil, fmt.Errorf("hf: encode embed request: %w", err)
	}
	body, err := c.do(ctx, url, "application/json", reqBody)
	if err != nil {
		return nil, err
	}

	// Batch shape: [[float, float, ...], [float, ...]]
	var batched [][]float32
	if err := json.Unmarshal(body, &batched); err == nil && len(batched) == len(inputs) {
		return batched, nil
	}
	// Single shape: [float, float, ...] — wrap.
	var single []float32
	if err := json.Unmarshal(body, &single); err == nil && len(inputs) == 1 {
		return [][]float32{single}, nil
	}
	return nil, fmt.Errorf("hf: unexpected embedding response shape (body: %s)", truncateBody(body))
}
