package gemini

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// roleToolMessage is the role a tool-result message carries.
const roleToolMessage = "tool"

// Translation between the runtime's message list and the Interactions API's
// flat step list. See interactions_types.go for why this API exists alongside
// generateContent.

// buildInteractionsInput replays a transcript as Interactions input.
//
// The runtime owns the transcript, so every call carries the full history and
// no previous_interaction_id is used. That keeps message log, compaction and
// state handoff working exactly as they do for generateContent.
func buildInteractionsInput(messages []types.Message) []any {
	// Assistant turns expand into several steps (thought, then one per
	// tool call), so reserve headroom rather than regrow per message.
	const stepsPerMessage = 3
	input := make([]any, 0, len(messages)*stepsPerMessage)

	for i := range messages {
		msg := &messages[i]
		switch msg.Role {
		case roleToolMessage:
			if step := toolResultStep(msg); step != nil {
				input = append(input, step)
			}

		case roleAssistant:
			// The model's thought steps must precede its function calls. A
			// history with a function_call and no thought is rejected outright,
			// so these are replayed from the opaque signatures captured when
			// the round was parsed.
			input = append(input, thoughtSteps(msg.Reasoning)...)

			for j := range msg.ToolCalls {
				input = append(input, functionCallStep(&msg.ToolCalls[j]))
			}
			if text := msg.GetContent(); text != "" {
				input = append(input, interactionsTextStep{Type: stepTypeText, Text: text})
			}

		default:
			if text := msg.GetContent(); text != "" {
				input = append(input, interactionsTextStep{Type: stepTypeText, Text: text})
			}
		}
	}

	return input
}

// thoughtSteps rebuilds the model's thought steps from the opaque signatures
// captured on the way in. Entries from other providers or other kinds are
// skipped, so a trace that has traveled through a different provider does not
// produce a malformed step.
func thoughtSteps(rt *types.ReasoningTrace) []any {
	if rt == nil {
		return nil
	}
	var out []any
	for i := range rt.Opaque {
		op := &rt.Opaque[i]
		if op.Kind != kindInteractionsThought || op.Data == "" {
			continue
		}
		out = append(out, interactionsThoughtStep{Type: stepTypeThought, Signature: op.Data})
	}
	return out
}

// functionCallStep converts one assistant tool call to a replayable step.
func functionCallStep(tc *types.MessageToolCall) interactionsFunctionCall {
	step := interactionsFunctionCall{
		Type:      stepTypeFunctionCall,
		ID:        tc.ID,
		Name:      tc.Name,
		Arguments: tc.Args,
	}
	// The signature is captured on the way in as provider metadata; replaying
	// it verbatim is required for Gemini 3 to accept the history.
	if sig := tc.ProviderMetadata[providerMetaThoughtSignature]; sig != "" {
		step.Signature = sig
	}
	return step
}

// toolResultStep converts a tool-result message to a function_result step.
// Returns nil when the message carries no result to report.
func toolResultStep(msg *types.Message) *interactionsFunctionResult {
	if msg.ToolResult == nil {
		return nil
	}
	tr := msg.ToolResult

	text := tr.Error
	if text == "" {
		for i := range tr.Parts {
			if tr.Parts[i].Text != nil {
				text += *tr.Parts[i].Text
			}
		}
	}
	if text == "" {
		text = msg.GetContent()
	}

	return &interactionsFunctionResult{
		Type:   stepTypeFunctionResult,
		Name:   tr.Name,
		CallID: tr.ID,
		Result: interactionsContent{Type: stepTypeText, Text: text},
	}
}

// buildInteractionsTools converts the tooling BuildTooling produced into the
// flat shape this API expects — not generateContent's nested
// functionDeclarations.
//
// It takes the built ProviderTools rather than the original descriptors so both
// APIs are fed from one source. Feeding them separately is how a tool set ends
// up subtly different between paths.
func buildInteractionsTools(tools providers.ProviderTools) []interactionsTool {
	decl, ok := tools.(geminiToolDeclaration)
	if !ok {
		if ptr, isPtr := tools.(*geminiToolDeclaration); isPtr && ptr != nil {
			decl = *ptr
		} else {
			return nil
		}
	}
	if len(decl.FunctionDeclarations) == 0 {
		return nil
	}
	out := make([]interactionsTool, 0, len(decl.FunctionDeclarations))
	for i := range decl.FunctionDeclarations {
		fn := &decl.FunctionDeclarations[i]
		out = append(out, interactionsTool{
			Type:        "function",
			Name:        fn.Name,
			Description: fn.Description,
			Parameters:  fn.Parameters,
		})
	}
	return out
}

// interactionsResponseFormat maps the caller's ResponseFormat.
//
// The schema is passed through UNSANITIZED, unlike generateContent: that path
// strips keywords such as additionalProperties because Gemini's responseSchema
// is an OpenAPI 3.0 subset, while this API accepts a full JSON Schema.
func interactionsResponseFormat(rf *providers.ResponseFormat) *interactionsRespFmt {
	if !wantsSchema(rf) {
		return nil
	}
	out := &interactionsRespFmt{Type: stepTypeText, MIMEType: applicationJSON}
	if rf.Type == providers.ResponseFormatJSONSchema && len(rf.JSONSchema) > 0 {
		out.Schema = rf.JSONSchema
	}
	return out
}

// parseInteractionsResponse extracts the spoken answer, tool calls and
// reasoning from a completed interaction.
//
// Reasoning comes from `thought` steps and never from `model_output`, so the
// two cannot be confused — a cleaner separation than generateContent, where a
// thought is a flag on an otherwise ordinary text part.
func parseInteractionsResponse(resp *interactionsResponse) (
	content string, toolCalls []types.MessageToolCall, reasoning *types.ReasoningTrace,
) {
	var thoughts string
	var opaque []types.OpaqueReasoning

	for i := range resp.Steps {
		step := &resp.Steps[i]
		switch step.Type {
		case stepTypeModelOutput, stepTypeText:
			content += step.text()

		case stepTypeThought:
			// A thought carries an opaque signature rather than readable text.
			// It is preserved so later rounds can replay it, which the API
			// requires, and is NOT surfaced as reasoning text — there is none.
			if step.Signature != "" {
				opaque = append(opaque, types.OpaqueReasoning{
					Provider: providerNameGemini,
					Kind:     kindInteractionsThought,
					Data:     step.Signature,
				})
			}
			thoughts += step.text()

		case stepTypeFunctionCall:
			callID := step.CallID
			if callID == "" {
				callID = step.ID
			}
			tc := types.MessageToolCall{
				ID:   callID,
				Name: step.Name,
				Args: step.Arguments,
			}
			if step.Signature != "" {
				tc.ProviderMetadata = map[string]string{
					providerMetaThoughtSignature: step.Signature,
				}
			}
			toolCalls = append(toolCalls, tc)
		}
	}

	if thoughts != "" || len(opaque) > 0 {
		reasoning = &types.ReasoningTrace{Text: thoughts, Opaque: opaque}
	}
	return content, toolCalls, reasoning
}
