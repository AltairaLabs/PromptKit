package integration

import (
	"context"
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
			{"key": "grader", "role": "llm", "description": "grades the toxicity guardrail", "required": true}
		]
	},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"validators": [
				{"type": "toxicity", "enabled": true, "message": "That request was blocked.",
				 "params": {"provider": "grader"}}
			]
		}
	}
}`

// judgeGuardrailUnnamedPackJSON declares the same guardrail without naming a
// provider for it — the pack author forgot the binding.
const judgeGuardrailUnnamedPackJSON = `{
	"id": "integration-judge-guardrail-unnamed",
	"version": "1.0.0",
	"description": "Judge-backed guardrail that names no provider",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"validators": [
				{"type": "toxicity", "enabled": true}
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
func TestJudgeGuardrail_OpenFailsWhenNoProviderIsNamed(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailUnnamedPackJSON)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err, "a judge-backed guardrail that names no provider must not open")
	assert.Contains(t, err.Error(), "params.provider")
}

// TestJudgeGuardrail_OpenFailsWhenTheHostBoundNothing: the pack names a
// provider and declares it, and the host supplied nothing for it.
func TestJudgeGuardrail_OpenFailsWhenTheHostBoundNothing(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailPackJSON)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "grader",
		"the error must name the logical provider the pack asked for")
}

// TestJudgeGuardrail_HostDefaultCoversAPackThatNamesNothing: a host driving a
// pack whose check names no provider can still supply a judge itself. It is a
// fallback, not the route — a pack that names a provider must have that name
// bound, and this one names none.
func TestJudgeGuardrail_HostDefaultCoversAPackThatNamesNothing(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailUnnamedPackJSON)
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

	assert.Positive(t, judge.calls, "the guardrail never consulted the host's judge")
	assert.NotEmpty(t, resp.Text(), "a clean verdict must not empty the turn")
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

// TestJudgeGuardrail_HostBindsTheNameThePackChose is the normal wiring: the
// pack names `grader`, the host binds a provider to that name, and the check
// grades through whatever the host bound. The turn has to actually run through
// that provider — Open() succeeding proves only that the gate passed, not that
// the check found the provider afterwards.
func TestJudgeGuardrail_HostBindsTheNameThePackChose(t *testing.T) {
	packPath := writePackFile(t, judgeGuardrailPackJSON)
	judgeRepo := &recordingRepo{response: `{"passed": true, "score": 1.0, "reasoning": "clean"}`}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithNamedProvider(sdk.ProviderSpec{
			ID:    "grader",
			Type:  "mock",
			Model: "mock-model",
			AdditionalConfig: map[string]any{
				"repository": mock.ResponseRepository(judgeRepo),
			},
		}),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err, "a provider bound to the name the pack chose should satisfy the guardrail")
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "Hello there")
	require.NoError(t, err)

	assert.Positive(t, judgeRepo.count(),
		"the provider the host bound to %q was never called; the check resolved nothing at turn time", "grader")
	assert.NotContains(t, resp.Text(), "That request was blocked.",
		"a clean verdict blocked the turn")
}
