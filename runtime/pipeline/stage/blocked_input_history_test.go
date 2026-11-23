package stage

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardedReply is a canned reply as runBeforeCallHooks (direction input) or an
// output block (direction output) records it.
func guardedReply(content, direction string) types.Message {
	return types.Message{
		Role: core.RoleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Validations: []types.ValidationResult{{
			ValidatorType: "content_excludes",
			Details:       map[string]any{"direction": direction},
		}},
	}
}

func blockedInputReply(content string) types.Message {
	return guardedReply(content, hooks.DirectionInput)
}

// markedReply is a canned reply as a block marks it, with no validation (a
// hook that names no validator).
func markedReply(content string) types.Message {
	return types.Message{
		Role: core.RoleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Meta: map[string]interface{}{types.MetaBlockedInput: true},
	}
}

func roleLines(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Role+":"+msgs[i].GetContent())
	}
	return out
}

func TestContextBuilder_SummaryExcludesBlockedInputTurn(t *testing.T) {
	summarizer := &recordingSummarizer{}
	got := runTurnBoundary(t,
		&ContextBuilderPolicy{TokenBudget: 80, Strategy: TruncateSummarize, Summarizer: summarizer},
		turnMsg(core.RoleUser, "forbidden"),
		blockedInputReply("refused"),
		turnMsg(core.RoleUser, "q0 "+strings.Repeat("w ", 79)),
		turnMsg(core.RoleAssistant, "a0"),
		turnMsg(core.RoleUser, "q1 w w w w"),
	)
	assert.Equal(t, []string{"system:[Summary of 2 earlier messages]: summary", "user:q1 w w w w"}, got)
	assert.Equal(t, []string{"user:q0 " + strings.Repeat("w ", 79), "assistant:a0"}, roleLines(summarizer.batch))
}

// Relevance truncation scores messages one by one, so it could keep a blocked
// user message and drop the canned reply that marks it; the provider request
// would then carry the blocked request with nothing to say it was blocked.
// Truncation works on the history without blocked turns.
func TestContextBuilder_RelevanceNeverKeepsHalfABlockedTurn(t *testing.T) {
	topic := newMockEmbeddingProvider()
	topic.embedFunc = func(texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i, x := range texts {
			out[i] = []float32{0, 1, 0}
			if strings.Contains(x, "apples") {
				out[i] = []float32{1, 0, 0}
			}
		}
		return out, nil
	}
	got := runTurnBoundary(t,
		&ContextBuilderPolicy{TokenBudget: 5, Strategy: TruncateLeastRelevant,
			RelevanceConfig: &RelevanceConfig{EmbeddingProvider: topic, MinRecentMessages: 1}},
		turnMsg(core.RoleUser, "pears question here"),
		turnMsg(core.RoleAssistant, "pears answer here"),
		turnMsg(core.RoleUser, "apples forbidden"),
		markedReply("refused refused refused"),
		turnMsg(core.RoleUser, "apples again"),
	)
	assert.Equal(t, []string{"user:pears question here", "user:apples again"}, got,
		"the budget the blocked turn would have used goes to the rest of the history")
}

func (p *reaskHistorySpy) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest,
	providerTools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.schemaSpyProvider.PredictWithTools(ctx, req, providerTools, choice)
}

func (p *requestRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.Provider.Predict(ctx, req)
}

func (p *requestRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.Provider.PredictStream(ctx, req)
}
