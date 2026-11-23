package openai

// tool_choice allowed_tools restricts a call to some of the tools it is sent
// without changing the tools array, which keeps the cached prompt prefix
// ("make only a subset of tools available across model requests, but not
// modify the list of tools you pass in, so you can maximize savings from
// prompt caching", https://developers.openai.com/api/docs/guides/function-calling).
// mode is "auto" or "required". The two APIs nest it differently:
//
//   - Chat Completions, ChatCompletionAllowedToolChoice:
//     {"type":"allowed_tools","allowed_tools":{"mode":"auto","tools":[{"type":"function","function":{"name":"x"}}]}}
//     (https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
//   - Responses, ToolChoiceAllowed:
//     {"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"x"}]}
//     (https://developers.openai.com/api/reference/resources/responses/methods/create)

const (
	typeAllowedTools  = "allowed_tools"
	keyMode           = "mode"
	keyTools          = "tools"
	allowedModeAuto   = "auto"
	allowedModeNeeded = "required"
)

// restrictedToolChoice returns the tool_choice that restricts a call to
// active, PredictionRequest.ActiveTools, under choice, and whether there is
// one: none for nil active (no restriction), on a hyperscaler platform (where
// allowed_tools is not documented; the pipeline refuses a call outside active
// anyway), and for a forced tool (which restricts the call further on its
// own). An empty active, or choice none, is tool_choice none.
func (p *Provider) restrictedToolChoice(choice string, active []string, responses bool) (any, bool) {
	if active == nil || p.platform != "" {
		return nil, false
	}
	if choice == toolChoiceNone || len(active) == 0 {
		return toolChoiceNone, true
	}
	mode := allowedModeAuto
	switch choice {
	case "", toolChoiceAuto:
	case toolChoiceRequired:
		mode = allowedModeNeeded
	default:
		return nil, false // a forced tool
	}
	tools := make([]map[string]any, len(active))
	for i, name := range active {
		if responses {
			tools[i] = map[string]any{keyType: typeFunction, keyName: name}
		} else {
			tools[i] = map[string]any{keyType: typeFunction, keyFunction: map[string]string{keyName: name}}
		}
	}
	if responses {
		return map[string]any{keyType: typeAllowedTools, keyMode: mode, keyTools: tools}, true
	}
	return map[string]any{
		keyType:          typeAllowedTools,
		typeAllowedTools: map[string]any{keyMode: mode, keyTools: tools},
	}, true
}
