package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// interactionsURL builds the endpoint for the Interactions API.
//
// Unlike generateContent the model is a request field rather than part of the
// path, so this is a single collection endpoint.
func (p *Provider) interactionsURL() string {
	// No credential in the URL on either platform — auth is applied as a
	// header by authCredential.
	return p.baseURL + interactionsPath
}

// buildInteractionsRequest assembles one stateless Interactions call: the whole
// transcript as input, the tools, and the caller's schema.
//
// It fails, before any HTTP call, when a message's media cannot be loaded or
// is a kind this API cannot carry.
func (p *ToolProvider) buildInteractionsRequest(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools,
) (interactionsRequest, error) {
	// Converted from req.Messages as given, so the message index in an error
	// or log line is the caller's index.
	input, err := p.buildInteractionsInput(ctx, req.Messages)
	if err != nil {
		return interactionsRequest{}, err
	}

	// generateContent carries the system prompt in a dedicated field. This API
	// has no such field, so it leads the transcript as ordinary text — the
	// alternative is dropping it, which would silently change behavior.
	if req.System != "" {
		input = slices.Insert(input, 0, any(interactionsTextStep{Type: stepTypeText, Text: req.System}))
	}

	out := interactionsRequest{
		Model:          p.model,
		Input:          input,
		Tools:          buildInteractionsTools(tools),
		ResponseFormat: interactionsResponseFormat(req.ResponseFormat),
	}

	// Thinking config has no equivalent field here — thought steps are returned
	// by default — so it is deliberately not forwarded rather than guessed at.
	return out, nil
}

// predictWithInteractions runs a non-streaming turn through the Interactions
// API. It exists because a response schema alongside tools only works here:
// on generateContent the schema constrains every turn, so it either fails the
// request or traps the model in a tool loop. See issue #1851.
func (p *ToolProvider) predictWithInteractions(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	predictResp := providers.PredictionResponse{}
	body, err := p.buildInteractionsRequest(ctx, req, tools)
	if err != nil {
		return predictResp, nil, err
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = body
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return predictResp, nil, fmt.Errorf("failed to marshal interactions request: %w", err)
	}

	respBytes, err := p.postJSON(ctx, p.interactionsURL(), raw)
	if err != nil {
		return predictResp, nil, err
	}

	var resp interactionsResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("failed to parse interactions response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("gemini interactions error: %s", resp.Error.Message)
	}

	content, toolCalls, reasoning := parseInteractionsResponse(&resp)

	predictResp.Content = content
	predictResp.Reasoning = reasoning
	predictResp.FinishReason = finishReasonForInteraction(len(toolCalls) > 0)
	if p.ShouldIncludeRawOutput() {
		predictResp.Raw = respBytes
	}
	if resp.Usage != nil {
		cost := p.CalculateCost(resp.Usage.TotalInputTokens, resp.Usage.TotalOutputTokens, 0)
		predictResp.CostInfo = &cost
	}

	return predictResp, toolCalls, nil
}
