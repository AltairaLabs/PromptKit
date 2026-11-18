package gemini

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A response schema alongside tools (#2771).
//
// Neither Gemini API can apply a schema to a round that may call tools. On
// generateContent the schema constrains every turn: Gemini 2.5 rejects the
// request, and Gemini 3 accepts it and keeps calling the tool. The Interactions
// API was meant to constrain only the answering turn, but measured live on
// gemini-3.7-flash it behaves the same way: with a schema set the model called
// the tool on every round (0 answers in 3 runs of 3 rounds, with the full
// transcript replayed and with previous_interaction_id alike), and without one
// it answered on the second round every time.
//
// So the tool round is sent without the schema. If it calls a tool, that is
// the result. If it answers, the loop is over, and the answer is asked for
// again with the schema and no tools — a request with nothing competing with
// the schema, which both APIs honor (3 of 3 live, each). The draft answer is
// discarded; the caller is billed for both calls.
//
// The pipeline's default structured-output mode already withholds the schema
// from tool rounds itself, so a caller going through it never sends both; this
// covers callers of the provider and the every_round mode.

// predictSchemaAfterTools runs a tool round without the schema and, when it
// answers rather than calling a tool, asks for the answer under the schema.
func (p *ToolProvider) predictSchemaAfterTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	draft, calls, err := p.predictRound(ctx, toolRoundRequest(req), tools, toolChoice)
	if err != nil || len(calls) > 0 {
		return draft, calls, err
	}

	final, calls, err := p.predictRound(ctx, req, nil, "")
	final.CostInfo = sumCost(draft.CostInfo, final.CostInfo)
	return final, calls, err
}

// streamSchemaAfterTools is predictSchemaAfterTools for a stream. The tool
// round's chunks are held until it ends: if it called a tool they are
// delivered, and if it answered they are dropped and the answer asked for
// under the schema is streamed instead, so the caller never sees the draft.
func (p *ToolProvider) streamSchemaAfterTools(
	ctx context.Context, req providers.PredictionRequest, tools any, toolChoice string,
) (<-chan providers.StreamChunk, error) {
	draft, err := p.streamRound(ctx, toolRoundRequest(req), tools, toolChoice)
	if err != nil {
		return nil, err
	}

	out := make(chan providers.StreamChunk)
	go func() {
		defer close(out)
		held, answered := holdToolRound(draft)
		if !answered {
			forwardChunks(ctx, out, held)
			return
		}

		final, err := p.streamRound(ctx, req, nil, "")
		if err != nil {
			forwardChunks(ctx, out, []providers.StreamChunk{{Error: err, FinishReason: providers.StringPtr("error")}})
			return
		}
		draftCost := held[len(held)-1].CostInfo
		for chunk := range final {
			if chunk.FinishReason != nil && chunk.Error == nil {
				addDraftCost(&chunk, draftCost)
			}
			if !forwardChunks(ctx, out, []providers.StreamChunk{chunk}) {
				drain(final)
				return
			}
		}
	}()
	return out, nil
}

// holdToolRound collects a tool round's chunks. answered reports whether the
// round ended cleanly with no tool call; the last chunk carries the round's
// accumulated tool calls.
func holdToolRound(ch <-chan providers.StreamChunk) (held []providers.StreamChunk, answered bool) {
	for chunk := range ch {
		held = append(held, chunk)
	}
	if len(held) == 0 {
		return held, false
	}
	last := held[len(held)-1]
	return held, last.Error == nil && last.FinishReason != nil && len(last.ToolCalls) == 0
}

// forwardChunks delivers chunks until ctx ends, reporting whether all were
// delivered.
func forwardChunks(ctx context.Context, out chan<- providers.StreamChunk, chunks []providers.StreamChunk) bool {
	for i := range chunks {
		select {
		case out <- chunks[i]:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// drain empties ch so the goroutine feeding it can finish.
func drain(ch <-chan providers.StreamChunk) {
	for range ch {
	}
}

// addDraftCost bills the discarded tool round on the final chunk, and on the
// final result built from it.
func addDraftCost(chunk *providers.StreamChunk, draft *types.CostInfo) {
	chunk.CostInfo = sumCost(draft, chunk.CostInfo)
	if res, ok := chunk.FinalResult.(*providers.PredictionResponse); ok && res != nil {
		withCost := *res
		withCost.CostInfo = chunk.CostInfo
		chunk.FinalResult = &withCost
	}
}

// toolRoundRequest is req without its response format.
func toolRoundRequest(req providers.PredictionRequest) providers.PredictionRequest {
	req.ResponseFormat = nil
	return req
}

// sumCost is a and b rolled into one; a nil part counts as zero.
func sumCost(a, b *types.CostInfo) *types.CostInfo {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	sum := base.AggregateCost(a, b)
	return &sum
}
