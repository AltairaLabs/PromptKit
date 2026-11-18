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
// discarded, but its usage is not: its cost and tokens, reasoning tokens
// included, are added to the answer's.
//
// The pipeline's default structured-output mode already withholds the schema
// from tool rounds itself, so a caller going through it never sends both. This
// covers callers of the provider and the every_round mode, which on Gemini
// therefore behaves like final_turn: a round that answers costs two calls.
//
// On a stream the tool round is held back whole until it ends, because only
// its end says whether it answered. A round that calls a tool reaches the
// caller in one burst once the model has finished it, and a round that answers
// is replaced by the schema answer's stream, which starts after a second call.
// Time to the first chunk grows accordingly.

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
// The stream's last chunk, whether it completes, fails or is canceled,
// carries the draft's usage with its own.
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
			if !forwardChunks(ctx, out, held) {
				out <- canceledChunk(ctx, lastChunk(held))
			}
			return
		}
		p.streamSchemaAnswer(ctx, req, lastChunk(held), out)
	}()
	return out, nil
}

// streamSchemaAnswer streams the answer asked for under the schema to out,
// adding usage, the discarded tool round's last chunk, to its last chunk.
func (p *ToolProvider) streamSchemaAnswer(
	ctx context.Context, req providers.PredictionRequest, usage providers.StreamChunk,
	out chan<- providers.StreamChunk,
) {
	final, err := p.streamRound(ctx, req, nil, "")
	if err != nil {
		out <- providers.StreamChunk{
			Error: err, FinishReason: providers.StringPtr("error"),
			CostInfo: usage.CostInfo, TokenCount: usage.TokenCount,
		}
		return
	}
	for chunk := range final {
		if chunk.FinishReason != nil || chunk.Error != nil {
			addDraftUsage(&chunk, usage)
		}
		if !forwardChunks(ctx, out, []providers.StreamChunk{chunk}) {
			out <- canceledChunk(ctx, usage)
			drain(final)
			return
		}
	}
}

// lastChunk is the last of chunks, or a zero chunk when there are none.
func lastChunk(chunks []providers.StreamChunk) providers.StreamChunk {
	if len(chunks) == 0 {
		return providers.StreamChunk{}
	}
	return chunks[len(chunks)-1]
}

// canceledChunk ends a stream whose context ended before all its chunks were
// delivered. It carries the usage of usage, the last chunk produced.
func canceledChunk(ctx context.Context, usage providers.StreamChunk) providers.StreamChunk {
	return providers.StreamChunk{
		Error: ctx.Err(), FinishReason: providers.StringPtr("canceled"),
		CostInfo: usage.CostInfo, TokenCount: usage.TokenCount,
	}
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

// addDraftUsage adds the discarded tool round's usage (its last chunk's cost
// and token count) to a terminal chunk, and to the final result built from it.
func addDraftUsage(chunk *providers.StreamChunk, draft providers.StreamChunk) {
	chunk.CostInfo = sumCost(draft.CostInfo, chunk.CostInfo)
	chunk.TokenCount += draft.TokenCount
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
