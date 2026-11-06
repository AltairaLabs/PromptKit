package gemini

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
//
// A message's media goes out inline as image, audio, video and document
// content items, loaded through one MediaLoader per message so the
// per-message caps apply as they do on generateContent. Media that cannot be
// loaded, or that this API cannot carry, is an error: sending the text alone
// would have the model answer without it (#2479). The exception is assistant
// media in a MIME type this API does not accept, such as the model's own TTS
// audio (audio/pcm): it is left out, with a debug line, because the message's
// text already carries its transcript and failing would end a conversation
// over the model's own output.
func (p *Provider) buildInteractionsInput(ctx context.Context, messages []types.Message) ([]any, error) {
	// Assistant turns expand into several steps (thought, then one per
	// tool call), so reserve headroom rather than regrow per message.
	const stepsPerMessage = 3
	input := make([]any, 0, len(messages)*stepsPerMessage)

	for i := range messages {
		steps, err := p.interactionsSteps(ctx, &messages[i], i)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message %d: %w", i, err)
		}
		input = append(input, steps...)
	}

	return input, nil
}

// interactionsSteps converts one message to the steps and content items that
// replay it.
func (p *Provider) interactionsSteps(ctx context.Context, msg *types.Message, msgIndex int) ([]any, error) {
	switch msg.Role {
	case roleToolMessage:
		step, err := p.toolResultStep(ctx, msg)
		if err != nil || step == nil {
			return nil, err
		}
		return []any{step}, nil

	case roleAssistant:
		// The model's thought steps must precede its function calls. A
		// history with a function_call and no thought is rejected outright,
		// so these are replayed from the opaque signatures captured when
		// the round was parsed.
		steps := thoughtSteps(msg.Reasoning)
		for j := range msg.ToolCalls {
			steps = append(steps, functionCallStep(&msg.ToolCalls[j]))
		}
		content, err := p.interactionsContentItems(ctx, msg, msgIndex)
		if err != nil {
			return nil, err
		}
		return append(steps, content...), nil

	default:
		return p.interactionsContentItems(ctx, msg, msgIndex)
	}
}

// interactionsContentItems converts a message's text and media to content
// items. A message with media keeps its parts' order; one without sends its
// text as a single item.
func (p *Provider) interactionsContentItems(
	ctx context.Context, msg *types.Message, msgIndex int,
) ([]any, error) {
	if !msg.HasMediaContent() {
		if text := msg.GetContent(); text != "" {
			return []any{interactionsTextStep{Type: stepTypeText, Text: text}}, nil
		}
		return nil, nil
	}

	loader := p.MediaLoader()
	items := make([]any, 0, len(msg.Parts))
	for i := range msg.Parts {
		item, err := p.interactionsPartItem(ctx, loader, msg, i, msgIndex)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, item)
		}
	}
	return items, nil
}

// interactionsPartItem converts part i of msg to a content item, or returns nil
// for a part with nothing to send. Assistant media in a MIME type this API does
// not accept is left out with a debug line rather than failing the request.
func (p *Provider) interactionsPartItem(
	ctx context.Context, loader *providers.MediaLoader, msg *types.Message, i, msgIndex int,
) (any, error) {
	part := &msg.Parts[i]
	if part.Type == types.ContentTypeText {
		if part.Text == nil || *part.Text == "" {
			return nil, nil
		}
		return interactionsTextStep{Type: stepTypeText, Text: *part.Text}, nil
	}

	item, err := p.interactionsMediaItem(ctx, loader, part, i)
	var unsupported *providers.UnsupportedContentError
	if msg.Role == roleAssistant && errors.As(err, &unsupported) {
		logger.Debug("gemini interactions: assistant media in a MIME type this API does not accept "+
			"left out of the replayed history",
			"role", msg.Role, "mime_type", unsupported.MIMEType, "message_index", msgIndex, "part_index", i)
		return nil, nil
	}
	if err != nil || item == nil {
		return nil, err
	}
	return *item, nil
}

// interactionsMediaItem converts one image, audio, video or document part to
// an inline content item, loading its data through loader. It returns nil for
// a part with nothing to send, which includes a part that is not media (a
// thinking part is replayed from Message.Reasoning instead), and an
// *providers.UnsupportedContentError for a MIME type this API does not accept.
func (p *Provider) interactionsMediaItem(
	ctx context.Context, loader *providers.MediaLoader, part *types.ContentPart, index int,
) (*interactionsMediaContent, error) {
	accepted, isMedia := interactionsMIMETypes[part.Type]
	if !isMedia || part.Media == nil {
		return nil, nil
	}

	mimeType := mediaMIMEType(*part)
	if !slices.Contains(accepted, mimeType) {
		return nil, &providers.UnsupportedContentError{
			Provider:    p.ID(),
			ContentType: part.Type,
			Message:     "the Gemini Interactions API does not accept " + part.Type + " format " + mimeType,
			PartIndex:   index,
			MIMEType:    mimeType,
		}
	}

	data, err := loader.GetBase64Data(ctx, part.Media)
	if err != nil {
		return nil, fmt.Errorf("%s part: failed to load %s data: %w", part.Type, part.Type, err)
	}
	return &interactionsMediaContent{Type: part.Type, Data: data, MIMEType: mimeType}, nil
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
//
// A result with media is sent in the list form, its text then its images. The
// list carries only text and image items, so an audio, video or document part
// is an *providers.UnsupportedContentError rather than a part left out.
func (p *Provider) toolResultStep(ctx context.Context, msg *types.Message) (*interactionsFunctionResult, error) {
	if msg.ToolResult == nil {
		return nil, nil
	}
	tr := msg.ToolResult
	text := toolResultText(msg)
	step := &interactionsFunctionResult{
		Type:   stepTypeFunctionResult,
		Name:   tr.Name,
		CallID: tr.ID,
		Result: interactionsContent{Type: stepTypeText, Text: text},
	}
	images, err := p.toolResultImages(ctx, tr)
	if err != nil || len(images) == 0 {
		return step, err
	}
	if text != "" {
		images = slices.Insert(images, 0, any(interactionsContent{Type: stepTypeText, Text: text}))
	}
	step.Result = images
	return step, nil
}

// toolResultImages loads a tool result's images, through one loader, as
// content items.
func (p *Provider) toolResultImages(ctx context.Context, tr *types.MessageToolResult) ([]any, error) {
	var images []any
	loader := p.MediaLoader()
	for i := range tr.Parts {
		part := &tr.Parts[i]
		if part.Type == types.ContentTypeText {
			continue // already in text
		}
		if part.Type != types.ContentTypeImage && part.Media != nil {
			return nil, &providers.UnsupportedContentError{
				Provider:    p.ID(),
				ContentType: part.Type,
				Message: "the Gemini Interactions API carries only text and images in a function result, " +
					"not " + part.Type + " (tool result " + tr.ID + ")",
				PartIndex: i,
				MIMEType:  mediaMIMEType(*part),
			}
		}
		item, err := p.interactionsMediaItem(ctx, loader, part, i)
		if err != nil {
			return nil, fmt.Errorf("tool result %s: %w", tr.ID, err)
		}
		if item != nil {
			images = append(images, *item)
		}
	}
	return images, nil
}

// toolResultText is a tool result's text: its error, else its text parts, else
// the message content.
func toolResultText(msg *types.Message) string {
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
	return text
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
