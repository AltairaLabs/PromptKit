package openai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// responsesOnlyMarkers are the phrases of the 404 OpenAI returns on
// v1/chat/completions for a model it serves only on v1/responses. Captured
// live (2026-11-20):
//
//	This model is only supported in v1/responses and not in v1/chat/completions.
//	    (gpt-5-pro, gpt-5-pro-2025-10-06)
//	This model is not supported in the v1/chat/completions endpoint. Use the
//	v1/responses endpoint instead.
//	    (gpt-5.3-codex)
//	This is not a chat model and thus not supported in the v1/chat/completions
//	endpoint. Did you mean to use v1/completions?
//	    (gpt-5.2-pro, gpt-5.4-pro, gpt-5.5-pro, o3-pro; each answers on v1/responses)
//
// The last one also names a legacy completions model, which this provider
// cannot serve on either API; sent to Responses, it fails there instead.
var responsesOnlyMarkers = []string{
	"only supported in v1/responses",
	"use the v1/responses endpoint",
	"not a chat model",
}

// currentAPIMode is the API a request goes to: Responses once Chat
// Completions has rejected the model as Responses-only, else apiMode.
func (p *Provider) currentAPIMode() APIMode {
	if p.apiMode == APIModeCompletions && p.learnedResponses != nil && p.learnedResponses.Load() {
		return APIModeResponses
	}
	return p.apiMode
}

// canLearnResponses reports whether a Chat Completions rejection may move this
// provider to Responses: the API was not declared (api_mode), the provider is
// on Chat Completions, and no platform pins the API (Azure, Bedrock and
// Vertex serve Chat Completions only through this provider).
func (p *Provider) canLearnResponses() bool {
	return !p.apiModeDeclared && p.platform == "" && p.learnedResponses != nil &&
		p.currentAPIMode() == APIModeCompletions
}

// isResponsesOnlyRejection reports whether err is Chat Completions' 404 for a
// model served only by the Responses API.
func isResponsesOnlyRejection(err error) bool {
	var httpErr *providers.ProviderHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		return false
	}
	body := strings.ToLower(httpErr.Body)
	for _, marker := range responsesOnlyMarkers {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// withResponsesFallback runs call on p, and when Chat Completions rejects the
// model as served only by the Responses API, runs call once more on a copy of
// p that uses the Responses API. Only when that resend succeeds does p switch
// to Responses for every later request; until then, other requests keep
// going to Chat Completions. When the resend fails too, the caller gets Chat
// Completions' error, with the Responses error joined after it, and p does
// not switch. A declared api_mode is never second-guessed.
func (p *Provider) withResponsesFallback(call func(*Provider) error) error {
	if !p.canLearnResponses() {
		return call(p)
	}
	err := call(p)
	if err == nil || !isResponsesOnlyRejection(err) {
		return err
	}
	trial := *p
	trial.apiMode = APIModeResponses
	if retryErr := call(&trial); retryErr != nil {
		return fmt.Errorf("%w (resent to the Responses API: %w)", err, retryErr)
	}
	p.learnedResponses.Store(true)
	logger.Warn("openai: Chat Completions rejected the model as Responses-only; using the Responses API "+
		"(set additional_config.api_mode: responses to skip the first attempt)",
		"provider", p.ID(), "model", p.model)
	return nil
}
