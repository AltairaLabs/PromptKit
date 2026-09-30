package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// finalChunks returns the chunks that carry a finish reason.
func finalChunks(chunks []providers.StreamChunk) []providers.StreamChunk {
	var out []providers.StreamChunk
	for _, c := range chunks {
		if c.FinishReason != nil {
			out = append(out, c)
		}
	}
	return out
}

// TestStreamFinishReason_SurvivesSeparateUsageChunk covers
// stream_options.include_usage: the finish_reason arrives on one chunk and the
// usage on a later, choice-less one. The final chunk must keep the real
// reason, carry the usage, and be the only final chunk.
func TestStreamFinishReason_SurvivesSeparateUsageChunk(t *testing.T) {
	cases := map[string]string{
		"length":     types.FinishReasonMaxOutputTokens,
		"tool_calls": types.FinishReasonToolUse,
		"stop":       types.FinishReasonStop,
	}
	for wire, want := range cases {
		t.Run(wire, func(t *testing.T) {
			body := chatPartial +
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"" + wire + "\"}]}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\n" +
				"data: [DONE]\n\n"
			chunks := runOverHTTP(t, body, newIncompleteTestProvider().streamResponse)

			finals := finalChunks(chunks)
			if len(finals) != 1 {
				t.Fatalf("got %d final chunks, want exactly 1", len(finals))
			}
			if got := *finals[0].FinishReason; got != want {
				t.Errorf("finish reason = %q, want %q", got, want)
			}
			if finals[0].CostInfo == nil || finals[0].CostInfo.OutputTokens != 3 {
				t.Errorf("final chunk lost the usage: %+v", finals[0].CostInfo)
			}
		})
	}
}

// TestStreamFinishReason_SurvivesDone covers a server that sends the
// finish_reason and then [DONE] with no usage chunk.
func TestStreamFinishReason_SurvivesDone(t *testing.T) {
	body := chatPartial +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
		"data: [DONE]\n\n"
	chunks := runOverHTTP(t, body, newIncompleteTestProvider().streamResponse)

	last := chunks[len(chunks)-1]
	if last.FinishReason == nil || *last.FinishReason != types.FinishReasonMaxOutputTokens {
		t.Fatalf("finish reason = %v, want %q", last.FinishReason, types.FinishReasonMaxOutputTokens)
	}
}
