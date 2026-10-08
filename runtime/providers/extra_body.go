package providers

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// ExtraBodyConfigKey is the additional_config key whose map is merged into the
// request body of the OpenAI-compatible chat providers (openai, vllm, ollama),
// like the OpenAI SDK's extra_body. It carries server-specific request
// parameters PromptKit has no field for, such as vLLM's chat_template_kwargs:
//
//	additional_config:
//	  extra_body:
//	    chat_template_kwargs:
//	      enable_thinking: false
//
// A field the provider sets itself (model, messages, tools, stream, ...) always
// wins; the colliding extra_body field is dropped with a warning.
const ExtraBodyConfigKey = "extra_body"

// ExtraBody returns the extra_body map from a provider's additional_config, or
// nil when there is none. A value that is not a JSON-encodable map is ignored
// with a warning naming the provider, so a mistyped extra_body is visible
// rather than silently doing nothing.
func ExtraBody(providerID string, additional map[string]any) map[string]any {
	raw, ok := additional[ExtraBodyConfigKey]
	if !ok || raw == nil {
		return nil
	}
	extra, ok := raw.(map[string]any)
	if !ok {
		logger.Warn("ignoring additional_config.extra_body: it must be a map of request fields",
			"provider", providerID, "type", fmt.Sprintf("%T", raw))
		return nil
	}
	if _, err := json.Marshal(extra); err != nil {
		logger.Warn("ignoring additional_config.extra_body: it cannot be encoded as JSON",
			"provider", providerID, "error", err)
		return nil
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// ApplyExtraBody adds each extra field to body unless body already sets it.
func ApplyExtraBody(providerID string, body, extra map[string]any) {
	for key, value := range extra {
		if _, set := body[key]; set {
			warnExtraBodyCollision(providerID, key)
			continue
		}
		body[key] = value
	}
}

// MarshalWithExtraBody marshals req, a JSON object, and merges extra into it as
// ApplyExtraBody does. With no extra fields it is json.Marshal.
func MarshalWithExtraBody(providerID string, req any, extra map[string]any) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil || len(extra) == 0 {
		return body, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("merging extra_body: request is not a JSON object: %w", err)
	}
	for key, value := range extra {
		if _, set := fields[key]; set {
			warnExtraBodyCollision(providerID, key)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("merging extra_body field %q: %w", key, err)
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

// extraBodyCollisionsWarned dedupes collision warnings to one per provider and
// field, since the same collision recurs on every request.
var extraBodyCollisionsWarned sync.Map

func warnExtraBodyCollision(providerID, key string) {
	if _, seen := extraBodyCollisionsWarned.LoadOrStore(providerID+"\x00"+key, struct{}{}); seen {
		return
	}
	logger.Warn("ignoring additional_config.extra_body field: the provider sets it itself",
		"provider", providerID, "field", key)
}
