package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// TestRequiresJudge_CoversTheJudgeBackedFamily pins which handlers a caller is
// told need a judge. The list is what `guardrails.CompileValidators` refuses to
// build without one, so a handler missing from it goes back to failing per turn
// — silently, in one of the two directions #1996 describes.
func TestRequiresJudge_CoversTheJudgeBackedFamily(t *testing.T) {
	judgeBacked := []any{
		&LLMJudgeHandler{},
		&LLMJudgeSessionHandler{},
		&LLMJudgeToolCallsHandler{},
		&BiasHandler{},
		&ToxicityHandler{},
		&RoleViolationHandler{},
		&FaithfulnessHandler{},
		&HallucinationHandler{},
		&AnswerRelevancyHandler{},
		&ContextualPrecisionHandler{},
		&ContextualRecallHandler{},
		&ContextualRelevancyHandler{},
		&PIILeakageHandler{},
	}

	for _, h := range judgeBacked {
		typed, ok := h.(interface{ Type() string })
		require.True(t, ok, "%T has no Type()", h)
		t.Run(typed.Type(), func(t *testing.T) {
			assert.True(t, RequiresJudge(h),
				"%T reaches the judge helpers but does not declare that it needs one", h)
		})
	}
}

// A handler that judges nothing must not be refused a build: the gate is scoped
// to checks that genuinely cannot run.
func TestRequiresJudge_FalseForDeterministicHandlers(t *testing.T) {
	assert.False(t, RequiresJudge(&ContainsHandler{}))
	assert.False(t, RequiresJudge(nil), "a nil handler is not judge-backed")
	assert.False(t, RequiresJudge("not a handler at all"))
}

func TestProviderJudge_NilProvider(t *testing.T) {
	assert.Nil(t, NewProviderJudge(nil),
		"a nil provider should yield a nil judge so callers can pass a lookup result through")

	var pj *ProviderJudge
	_, err := pj.Judge(context.Background(), JudgeOpts{Content: "x", Criteria: "y"})
	require.Error(t, err, "a nil judge must report that it is unconfigured, not panic")
	assert.Contains(t, err.Error(), "not configured")
}

// TestProviderJudge_UsesTheSuppliedProvider: the judge calls the host's
// provider rather than building one of its own, which is the whole difference
// from SpecJudgeProvider.
func TestProviderJudge_UsesTheSuppliedProvider(t *testing.T) {
	provider := mock.NewProviderWithRepository("host-judge", "mock-model", false,
		mock.NewInMemoryMockRepository(`{"passed": true, "score": 0.75, "reasoning": "acceptable"}`))

	judge := NewProviderJudge(provider)
	require.NotNil(t, judge)

	result, err := judge.Judge(context.Background(), JudgeOpts{
		Content:  "some output",
		Criteria: "is it acceptable",
	})

	require.NoError(t, err)
	assert.InDelta(t, 0.75, result.Score, 0.001)
	assert.True(t, result.Passed)
	assert.Equal(t, "acceptable", result.Reasoning)
}

// An unreadable verdict is an error, not a fabricated score — the same contract
// SpecJudgeProvider has, which is why both share judgeWithProvider.
func TestProviderJudge_UnparseableVerdictIsAnError(t *testing.T) {
	provider := mock.NewProviderWithRepository("host-judge", "mock-model", false,
		mock.NewInMemoryMockRepository("I think it's fine, honestly"))

	_, err := NewProviderJudge(provider).Judge(context.Background(), JudgeOpts{
		Content:  "some output",
		Criteria: "is it acceptable",
	})

	require.Error(t, err)
}
