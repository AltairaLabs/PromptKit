package stage

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// StructuredOutputMode selects when a caller's ResponseFormat is applied to a
// tool loop.
//
// A response schema competes with tool calling. Applied to every round it
// suppresses tool use: silently, intermittently, and more the more work the
// task requires. Measured on an identical 7-tool task, mean tools called
// (n=5, no schema -> schema):
//
//	claude-sonnet-5      7.0 -> 7.0   (no detectable effect)
//	gpt-5                6.8 -> 6.6   (within its own baseline noise)
//	claude-sonnet-4-6    7.0 -> 5.8   (one run did almost no work)
//	gemini-3.7-flash     terminates 5/5 -> 1/5
//
// On a production underwriting pack the same model that scores 5.8 here reached
// tool_use in 1 run out of 5. The effect scales with task complexity, so these
// numbers understate it. Every failure arrives as a successful HTTP 200.
//
// The fix is to constrain only the turn that produces the final answer, which
// is what Anthropic's stronger models already do server-side. Doing it in the
// stage rather than per provider means no per-model support table: the rule is
// mechanical and every provider gets it, including ones added later.
//
// Gemini cannot carry a schema on a round with tools on either of its APIs,
// so its provider applies the same rule itself: under every_round it
// sends each tool round without the schema and asks a round that answers again
// under it, which costs two calls per answering round, as final_turn does.
type StructuredOutputMode string

const (
	// StructuredOutputFinalTurn withholds ResponseFormat during tool-calling
	// rounds and re-asks the final answer under the schema. Default.
	StructuredOutputFinalTurn StructuredOutputMode = "final_turn"

	// StructuredOutputEveryRound sends ResponseFormat on every round. This is the
	// older behavior, from before the final-turn rule existed.
	//
	// This is an escape hatch, not a supported alternative: it is the
	// configuration measured above, and on two of the four models tested it
	// loses tool calls. It exists so an operator can pin the old behavior
	// without waiting on a release, not because it is a reasonable choice.
	StructuredOutputEveryRound StructuredOutputMode = "every_round"

	// structuredOutputUnset means no explicit choice; the default applies.
	structuredOutputUnset StructuredOutputMode = ""
)

// ParseStructuredOutputMode reads a configured mode string.
//
// Mirrors the provider api_mode convention: an unrecognized value is ignored
// with a warning rather than forwarded, since guessing here silently changes
// whether a caller's schema constrains tool-calling rounds.
func ParseStructuredOutputMode(raw string) StructuredOutputMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "final_turn", "finalturn", "final":
		return StructuredOutputFinalTurn
	case "every_round", "everyround", "legacy":
		return StructuredOutputEveryRound
	case "":
		return structuredOutputUnset
	default:
		logger.Warn("ignoring unrecognized structured_output_mode",
			"value", raw, "accepted", "final_turn|every_round")
		return structuredOutputUnset
	}
}

// resolve returns the effective mode, defaulting to final_turn.
func (m StructuredOutputMode) resolve() StructuredOutputMode {
	if m == structuredOutputUnset {
		return StructuredOutputFinalTurn
	}
	return m
}

// withholdsSchema reports whether this turn's ResponseFormat must be kept off
// the tool-loop rounds and applied only to a final re-ask.
//
// Three conditions, all necessary:
//   - a schema was actually requested — otherwise there is nothing to withhold;
//   - the turn carries tools — a schema cannot suppress tool calling when there
//     are no tools, so a tool-free turn keeps today's single constrained call
//     and costs no extra round trip;
//   - the mode is final_turn.
func (s *ProviderStage) withholdsSchema(providerTools interface{}) bool {
	if s.config == nil || s.config.ResponseFormat == nil || providerTools == nil {
		return false
	}
	return s.config.StructuredOutputMode.resolve() == StructuredOutputFinalTurn
}

// roundResponseFormat returns the ResponseFormat a tool-loop round should carry.
//
// nil while a schema is being withheld: the rounds run unconstrained so tool
// calling is not competing with the schema, and afterRound re-asks the final
// answer under it.
func (s *ProviderStage) roundResponseFormat(providerTools interface{}) *providers.ResponseFormat {
	if s.withholdsSchema(providerTools) {
		return nil
	}
	if s.config == nil {
		return nil
	}
	return s.config.ResponseFormat
}

// reaskCall issues the re-ask on whichever transport the loop is running.
//
// On the streaming path it MUST stream. Every round's text is suppressed while
// a schema is withheld, so the re-ask is the only thing a streaming consumer
// ever receives — issuing it non-streaming delivers the answer as a message
// with no text chunks at all, and a caller rendering the stream sees nothing.
// Unit tests could not see this: asserting "no loop text leaked" is satisfied
// just as well by a stream that carries nothing whatsoever.
//
// The streamed re-ask's elements are held, not sent: held returns them for
// the caller to send once the re-ask succeeded. Its answer is stored under
// the replaced reply's id, which its text carries; a re-ask that fails keeps
// the replaced reply under that id instead, so none of the failed answer may
// reach the caller under it.
//
// late, when set, delivers usage a cut stream reports after the call ended
// (streamedRound.lateUsage); the caller reports it once the call completes.
func (tl *toolLoop) reaskCall(
	ctx context.Context, req providers.PredictionRequest, rr roundRef,
) (resp providers.PredictionResponse, late <-chan *types.CostInfo, held []StreamElement, err error) {
	if tl.output == nil {
		resp, err = tl.reaskPredict(ctx, req)
		return resp, nil, nil, err
	}

	// suppressText is false here: this IS the answer, and it is the only text
	// the consumer is going to get.
	streamChan, err := tl.stage.startStreamingRequest(ctx, req, nil, "")
	if err != nil {
		return providers.PredictionResponse{}, nil, nil, err
	}
	hold := make(chan StreamElement)
	collected := make(chan []StreamElement, 1)
	go func() {
		var elems []StreamElement
		for elem := range hold {
			elems = append(elems, elem)
		}
		collected <- elems
	}()
	got, err := tl.stage.processStreamChunks(ctx, streamChan, hold, rr, false)
	close(hold)
	held = <-collected
	if err != nil {
		return providers.PredictionResponse{}, nil, nil, err
	}
	return providers.PredictionResponse{
		Content:      got.content,
		CostInfo:     got.costInfo,
		Reasoning:    got.reasoning,
		FinishReason: got.finishReason,
	}, got.lateUsage, held, nil
}

// reaskPredict issues a non-streaming re-ask, choosing the same request path
// the stage's rounds use.
//
// It must go through the TOOL path whenever the transcript contains tool
// messages, even though it offers no tools: some providers' plain-Predict
// serializer is tool-blind and rejects the tool-result roles outright — Claude
// answers 400 `Unexpected role "tool"`. (OpenAI's plain path now shares the
// tool path's serializer and no longer needs this.) Passing nil tools through
// the tool-aware path gets the transcript serialized correctly while leaving
// the schema uncontested, which is the whole point of the re-ask.
//
// useToolPath already encodes this rule for the rounds; reusing it keeps the
// re-ask from drifting from them.
func (tl *toolLoop) reaskPredict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	s := tl.stage
	toolProvider, supportsTools := s.callProvider().(providers.ToolSupport)
	if s.useToolPath(nil, req.Messages, supportsTools) {
		resp, _, err := toolProvider.PredictWithTools(ctx, req, nil, "")
		return resp, err
	}
	return s.callProvider().Predict(ctx, req)
}

// reaskUnderSchema regenerates the loop's closing answer with the caller's
// ResponseFormat applied and no tools offered.
//
// Called once, when a round returns no tool calls — the only point at which the
// loop is known to be over. The unconstrained answer that revealed it is
// discarded and asked again under the schema, which is why the loop can afford
// to learn its own ending late.
//
// Tools are omitted deliberately. The model has already decided it is done;
// re-offering tools would reopen a loop this call exists to close, and
// withholding them is what leaves the schema uncontested.
//
// A failure here returns the unconstrained answer rather than failing the turn.
// Prose where JSON was asked for is a caller-visible defect, but it is a
// smaller one than losing a completed tool loop's work outright.
func (tl *toolLoop) reaskUnderSchema(ctx context.Context, rr roundRef) {
	s := tl.stage
	prov := s.callProvider() // one read: events and cost name the provider that served the call
	if len(tl.messages) == 0 {
		return
	}
	last := len(tl.messages) - 1
	prior := ExcludeFromModelContext(tl.messages[:last])

	req := providers.PredictionRequest{
		System:         tl.acc.systemPrompt,
		Messages:       prior,
		Seed:           s.config.Seed,
		ResponseFormat: s.config.ResponseFormat,
		Metadata:       tl.acc.metadata,
	}
	s.applySampling(&req)
	req.NormalizeMessages()

	// Reset the idle watchdog, exactly as executeRound does at the top of every
	// round. Without this the re-ask shares the previous round's window: that
	// round's reset, then its tool executions, then this call all have to fit
	// inside one idle timeout (30s by default), and a slow re-ask trips it
	// mid-flight as "http2: response body closed". A round-trip to a provider
	// is activity, so it gets its own window.
	ResetIdleFromContext(ctx)

	// The re-ask is a real provider call and is emitted as one. Without this a
	// consumer counting provider.call.* under-reports by one call per
	// structured-output turn, and any cost or latency derived from those events
	// is short by the whole final answer. Its own call ID keeps it distinct
	// from the round that preceded it.
	callID := newProviderCallID()
	if tl.stage.emitter != nil {
		tl.stage.emitter.ProviderCallStartedCtx(ctx, &events.ProviderCallStartedData{
			Provider: prov.ID(),
			Model:    prov.Model(),
			Source:   s.config.Source,
			Labels:   s.config.Labels,
			Round:    rr.round,
			CallID:   callID,
		})
	}

	// The re-ask's answer replaces the last message and is stored under its
	// id, so its text, and the end of the call, name that id.
	rr.messageID = tl.messages[last].ID()

	started := timeNow()
	resp, late, held, err := tl.reaskCall(ctx, req, rr)
	tl.endReask(ctx, rr.messageID, resp.FinishReason, held, err)
	if err != nil {
		if tl.stage.emitter != nil {
			tl.stage.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: prov.ID(),
				Model:    prov.Model(),
				Error:    err,
				Duration: timeNow().Sub(started),
				Source:   s.config.Source,
				Labels:   s.config.Labels,
				Round:    rr.round,
				CallID:   callID,
			})
		}
		// Degrade rather than fail: prose where JSON was asked for is a defect,
		// but a smaller one than discarding a completed tool loop's work.
		//
		// Mark it on the message. A silent fallback would hand the caller prose
		// with nothing to distinguish it from a model that simply answered that
		// way — the same unobservable-success failure this whole mode exists to
		// remove. Callers that require conforming output can detect it here;
		// the log alone is not reachable from a response.
		logger.Warn("structured output: final-turn re-ask failed; returning the unconstrained answer",
			"error", err)
		tl.markSchemaUnapplied("final-turn re-ask failed: " + err.Error())
		return
	}
	duration := timeNow().Sub(started)

	if resp.CostInfo != nil {
		if resp.CostInfo.ProviderName == "" {
			resp.CostInfo.ProviderName = prov.Name()
		}
		if resp.CostInfo.Model == "" {
			resp.CostInfo.Model = prov.Model()
		}
		if resp.CostInfo.Capability == "" {
			resp.CostInfo.Capability = string(prov.Type())
		}
		if resp.CostInfo.Latency == 0 {
			resp.CostInfo.Latency = duration
		}
	}

	if tl.stage.emitter != nil {
		completed := &events.ProviderCallCompletedData{
			Provider:     prov.ID(),
			Model:        prov.Model(),
			Duration:     duration,
			FinishReason: resp.FinishReason,
			Source:       s.config.Source,
			Labels:       s.config.Labels,
			Round:        rr.round,
			CallID:       callID,
		}
		if resp.CostInfo != nil {
			completed.InputTokens = resp.CostInfo.InputTokens
			completed.OutputTokens = resp.CostInfo.OutputTokens
			completed.CachedTokens = resp.CostInfo.CachedTokens
			completed.Cost = resp.CostInfo.TotalCost
		}
		tl.stage.emitter.ProviderCallCompletedCtx(ctx, completed)
	}
	tl.stage.reportLateUsage(ctx, late, prov, roundRef{round: rr.round, providerCallID: callID})

	constrained := types.Message{
		Role:         roleAssistant,
		Content:      resp.Content,
		Parts:        resp.Parts,
		Reasoning:    resp.Reasoning,
		Timestamp:    timeNow(),
		LatencyMs:    duration.Milliseconds(),
		CostInfo:     resp.CostInfo,
		FinishReason: resp.FinishReason,
	}
	withStableID(&constrained, rr.messageID)
	// Carry the workflow attribution of the answer being replaced. The re-ask
	// is the same logical turn, produced by the same state; re-deriving it
	// would read whatever state a mid-loop handoff has since moved to.
	if replaced := tl.messages[last]; replaced.Meta != nil {
		if meta, ok := replaced.Meta[workflowStateMetaKey]; ok {
			if constrained.Meta == nil {
				constrained.Meta = map[string]interface{}{}
			}
			constrained.Meta[workflowStateMetaKey] = meta
		}
	}
	// Accrue the spend, but note it is NOT re-checked against MaxCostUSD: that
	// check runs in afterRound before this call, so a turn can exceed its cap by
	// one re-ask. Deliberate — the loop has already finished its work, and
	// failing the turn here would discard it to enforce a bound that the extra
	// call was always going to cross. The cost is still reported, so the
	// overshoot is visible rather than hidden.
	if constrained.CostInfo != nil {
		tl.cumulativeCost += constrained.CostInfo.TotalCost
	}
	tl.messages[last] = constrained
}

// endReask sends what a streamed re-ask leaves the caller, then ends it with
// its ProviderCallEnd element, as every streamed provider call of a per-turn
// execution ends. A re-ask that succeeded sends its held elements, and its
// end names messageID, under which its answer is stored. One that failed
// keeps the unconstrained answer under messageID: that answer's text is
// sent under it, as the stored message holds it, in place of anything of
// the re-ask, and the end, types.FinishReasonError, names an id of its own,
// under which nothing is stored.
func (tl *toolLoop) endReask(
	ctx context.Context, messageID, finishReason string, held []StreamElement, err error,
) {
	if tl.output == nil {
		return
	}
	var fwd cancelForwarder
	if err != nil {
		// reaskCall holds nothing of a failed re-ask.
		if text := tl.messages[len(tl.messages)-1].GetContent(); text != "" {
			elem := textDeltaElement(text, nil)
			elem.Meta.MessageID = messageID
			held = append(held, elem)
		}
		messageID, finishReason = types.NewMessageID(), types.FinishReasonError
	}
	for i := range held {
		fwd.forward(ctx, tl.output, held[i])
	}
	if !tl.reportsRounds {
		return
	}
	sendProviderCallEnd(ctx, &providers.ProviderCallEnd{MessageID: messageID, FinishReason: finishReason}, tl.output)
}

// SchemaUnappliedMetaKey marks an assistant message that a configured
// ResponseFormat was NOT applied to, so its content is the loop's unconstrained
// answer rather than schema-shaped output. The value says why.
//
// Two causes reach it: the re-ask failed at the provider, or the tool loop
// exhausted its rounds and never produced a final turn to constrain.
//
// Exported because detecting it is a caller's decision. Returning prose is the
// right trade against losing a completed tool loop's work, but only if the
// caller can tell it happened. An unmarked fallback is indistinguishable from
// a model that answered in prose, which is the unobservable-success
// failure this whole mode exists to remove.
const SchemaUnappliedMetaKey = "structured_output_schema_unapplied"

// markSchemaUnapplied stamps the un-replaced answer so the gap is visible.
func (tl *toolLoop) markSchemaUnapplied(reason string) {
	if len(tl.messages) == 0 {
		return
	}
	last := len(tl.messages) - 1
	if tl.messages[last].Meta == nil {
		tl.messages[last].Meta = map[string]interface{}{}
	}
	tl.messages[last].Meta[SchemaUnappliedMetaKey] = reason
}
