package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, toolChoice string,
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
	out.Generation = p.interactionsGenerationConfig(req)
	if len(out.Tools) > 0 {
		choice := interactionsToolChoice(p.fitToolChoice(toolChoice, interactionsToolNames(out.Tools)))
		if choice != nil {
			out.Generation["tool_choice"] = choice
		}
	}

	return out, nil
}

// interactionsGenerationConfig carries the sampling parameters the
// generateContent path sends (provider defaults, overridden per request) in
// the Interactions API's generation_config. max_output_tokens counts thinking
// tokens too, as maxOutputTokens does on generateContent.
//
// generation_config has no top_k, presence_penalty or frequency_penalty: the
// API answers each with HTTP 400 "Unknown parameter" (probed live). A request
// that sets one gets the warning generateContent gives for a parameter it
// does not send.
func (p *ToolProvider) interactionsGenerationConfig(req providers.PredictionRequest) map[string]any {
	temperature, topP, maxTokens := p.applyRequestDefaults(req)
	gen := map[string]any{wireKeyTemperature: temperature}
	if topP > 0 {
		gen["top_p"] = topP
	}
	if maxTokens > 0 {
		gen["max_output_tokens"] = maxTokens
	}
	providers.WarnUnsentParams(p.ID(), &req,
		providers.ParamTopK, providers.ParamFrequencyPenalty, providers.ParamPresencePenalty)
	p.applyInteractionsThinking(gen)
	return gen
}

// interactionsThinkingSummariesAuto asks the API for thought summaries.
const interactionsThinkingSummariesAuto = "auto"

// interactionsBudgetWarned keys the provider IDs warned that thinking_budget
// is not sent, so the warning is logged once per provider.
var interactionsBudgetWarned sync.Map

// applyInteractionsThinking sets the provider's thinking config on the
// Interactions API's generation_config.
//
//   - thinking_level is sent as thinking_level: minimal, low, medium or
//     high (see thinkingLevels; xhigh is dropped). Each model takes a subset
//     and answers another level with HTTP 400, as on generateContent:
//     gemini-3.7-flash rejects minimal, and gemini-2.5-flash takes only low
//     and high here.
//   - include_thoughts is sent as thinking_summaries "auto". Without it the
//     API returns thought steps with a signature and no summary.
//   - thinking_budget has no field here (HTTP 400 "Unknown parameter
//     'thinking_budget'") and no level it equals, so it is not sent and a
//     warning says so. With a level configured the level wins and the budget
//     is dropped, as on generateContent.
func (p *Provider) applyInteractionsThinking(gen map[string]any) {
	switch {
	case p.thinkingLevel != nil:
		gen["thinking_level"] = *p.thinkingLevel
	case p.thinkingBudget != nil:
		if _, warned := interactionsBudgetWarned.LoadOrStore(p.ID(), struct{}{}); !warned {
			logger.Warn("gemini: thinking_budget is not sent: the Interactions API takes thinking_level instead",
				"provider", p.ID(), "thinking_budget", *p.thinkingBudget)
		}
	}
	if p.includeThoughts {
		gen["thinking_summaries"] = interactionsThinkingSummariesAuto
	}
}

// interactionsModeAny is the Interactions API tool_choice mode that forces a
// call (generateContent's ANY).
const interactionsModeAny = "any"

// interactionsToolChoice maps a PromptKit tool choice to the Interactions
// API's generation_config.tool_choice: a mode string, or allowed_tools to
// restrict the call to a named tool (mode "any" alone lets the model pick any
// declared tool). It returns nil for auto, the API's default, so the request
// carries no tool_choice.
func interactionsToolChoice(toolChoice string) any {
	if name := namedToolChoice(toolChoice); name != "" {
		return map[string]any{
			"allowed_tools": map[string]any{wireKeyMode: interactionsModeAny, "tools": []string{name}},
		}
	}
	switch functionCallingMode(toolChoice) {
	case functionCallingModeAny:
		return interactionsModeAny
	case functionCallingModeNone:
		return toolChoiceNone
	default:
		return nil
	}
}

// predictWithInteractions runs a non-streaming turn through the Interactions
// API, for a provider configured with api_mode interactions.
func (p *ToolProvider) predictWithInteractions(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	predictResp := providers.PredictionResponse{}
	body, err := p.buildInteractionsRequest(ctx, req, tools, toolChoice)
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
	predictResp.FinishReason = finishReasonForInteraction(resp.Status, len(toolCalls) > 0)
	if p.ShouldIncludeRawOutput() {
		predictResp.Raw = respBytes
	}
	if resp.Usage != nil {
		cost := p.costFromUsage(resp.Usage.geminiUsage())
		predictResp.CostInfo = &cost
	}

	return predictResp, toolCalls, nil
}
