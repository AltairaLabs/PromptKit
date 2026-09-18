package integration

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// judgeGuardrailPackJSON declares a judge-backed guardrail the way the
// handlers' own docs say they are meant to be wired, and states the judge it
// needs in the requires block — the pack names a dependency, the host fulfils
// it.
const judgeGuardrailPackJSON = `{
	"id": "integration-judge-guardrail",
	"version": "1.0.0",
	"description": "Pack with a judge-backed guardrail",
	"requires": {
		"providers": [
			{"key": "judge", "role": "llm", "description": "grades the toxicity guardrail", "required": true}
		]
	},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"validators": [
				{"type": "toxicity", "enabled": true, "message": "That request was blocked."}
			]
		}
	}
}`

// countingJudge reports a clean verdict and counts the calls, so a test can
// tell "the guardrail ran and passed" from "the guardrail never ran".
type countingJudge struct{ calls int }

//nolint:gocritic // JudgeOpts is passed by value by the JudgeProvider interface
func (j *countingJudge) Judge(_ context.Context, _ handlers.JudgeOpts) (*handlers.JudgeResult, error) {
	j.calls++
	return &handlers.JudgeResult{Passed: true, Score: 1.0, Reasoning: "clean"}, nil
}

// TestJudgeGuardrail_OpenFailsWithoutJudge is #1996 at the seam that matters to
// a consumer. A toxicity guardrail with no judge used to open successfully and
// then block EVERY turn, reporting a content violation — the pack's stated
// requirement going unmet looked like an over-aggressive model.
func TestJudgeGuardrail_OpenFailsWithoutJudge(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailPackJSON)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err, "a judge-backed guardrail with no judge must not open")
	assert.Contains(t, strings.ToLower(err.Error()), "judge")
}

// TestJudgeGuardrail_RunsWithHostSuppliedJudge: with the judge supplied, the
// guardrail runs on real turns and a clean verdict does not block.
func TestJudgeGuardrail_RunsWithHostSuppliedJudge(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailPackJSON)
	judge := &countingJudge{}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithJudgeProvider(judge),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "Hello there")
	require.NoError(t, err)

	assert.Positive(t, judge.calls, "the guardrail never consulted the judge")
	assert.NotContains(t, resp.Text(), "That request was blocked.",
		"a clean verdict blocked the turn — the 0.0-score failure mode is back")
}

// recordingRepo is a mock response source that counts the calls made to the
// provider it backs, so a test can prove the JUDGE provider — not the agent —
// was the one consulted.
type recordingRepo struct {
	mu       sync.Mutex
	calls    int
	response string
}

func (r *recordingRepo) GetResponse(_ context.Context, _ mock.ResponseParams) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.response, nil
}

func (r *recordingRepo) GetTurn(ctx context.Context, params mock.ResponseParams) (*mock.Turn, error) {
	text, err := r.GetResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	return &mock.Turn{Type: "text", Content: text}, nil
}

func (r *recordingRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestJudgeGuardrail_PooledJudgeProviderSatisfiesTheRequirement: the normal
// wiring, where the host answers the pack's requires block by registering a
// provider under that key rather than passing a judge object. The turn has to
// actually run through that provider — Open() succeeding proves only that the
// gate was satisfied, not that the guardrail found the judge afterwards.
func TestJudgeGuardrail_PooledJudgeProviderSatisfiesTheRequirement(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailPackJSON)
	judgeRepo := &recordingRepo{response: `{"passed": true, "score": 1.0, "reasoning": "clean"}`}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithLLMProvider(sdk.ProviderSpec{
			ID:    sdk.JudgeProviderKey,
			Type:  "mock",
			Model: "mock-model",
			AdditionalConfig: map[string]any{
				"repository": mock.ResponseRepository(judgeRepo),
			},
		}),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err, "a provider registered under %q should satisfy the guardrail",
		sdk.JudgeProviderKey)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "Hello there")
	require.NoError(t, err)

	assert.Positive(t, judgeRepo.count(),
		"the pooled judge provider was never called; the guardrail found no judge at turn time")
	assert.NotContains(t, resp.Text(), "That request was blocked.",
		"a clean verdict blocked the turn")
}
