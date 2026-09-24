package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Every route a check can reach a provider by, exercised end to end. Resolution
// working in one path says nothing about the others: they attach the binding
// from different places — the pipeline context, the eval middleware's own
// context, and the offline path's caller-supplied one.

// --- a binding a test can hand to the offline path ------------------------

type testBinding struct {
	llm        providers.Provider
	classifier inference.Provider
}

func (b testBinding) LLM(key string) (providers.Provider, error) {
	if b.llm == nil || key != "grader" {
		return nil, evals.ErrUnboundKey
	}
	return b.llm, nil
}

func (b testBinding) Inference(key string) (inference.Provider, error) {
	if b.classifier == nil || key != "screener" {
		return nil, evals.ErrUnboundKey
	}
	return b.classifier, nil
}

// TestProviderBinding_OfflineEvaluateResolvesNamedProviders covers sdk.Evaluate,
// which has no conversation and no provider pool — so a pack whose checks name
// their providers had nothing to resolve against until the caller could supply
// a binding.
func TestProviderBinding_OfflineEvaluateResolvesNamedProviders(t *testing.T) {
	classifier := &stubTextClassifier{}

	results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
		PackData:             []byte(classifyCheckPack("screener")),
		PromptName:           "chat",
		SkipSchemaValidation: true,
		ProviderBinding:      testBinding{classifier: classifier},
		Messages: []types.Message{
			{Role: "user", Content: "what a lovely day"},
			{Role: "assistant", Content: "glad to hear it"},
		},
	})

	require.NoError(t, err)
	require.NotEmpty(t, results, "the pack's eval did not run at all")
	assert.Empty(t, results[0].Error, "the eval could not resolve its provider: %s", results[0].Error)
	assert.Positive(t, classifier.count(),
		"the offline path never reached the classifier the caller bound")
}

// The same path, with nothing bound. A check that NAMED a provider and could
// not get it must ERROR, not skip: a skip scores 1.0 and passes, so a safety
// check that never ran would report clean — the failure this whole mechanism
// exists to prevent, one level up.
func TestProviderBinding_OfflineEvaluateWithoutABinding(t *testing.T) {
	results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
		PackData:             []byte(classifyCheckPack("screener")),
		PromptName:           "chat",
		SkipSchemaValidation: true,
		Messages: []types.Message{
			{Role: "user", Content: "what a lovely day"},
			{Role: "assistant", Content: "glad to hear it"},
		},
	})

	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.NotEmpty(t, results[0].Error,
		"a named-but-unresolvable provider must fail the check, not skip it into a pass")
	assert.False(t, results[0].Skipped, "skipped scores 1.0 and passes")
	assert.Contains(t, results[0].Error, "screener",
		"the failure must name the provider the check asked for")
}

// --- session-trigger evals ------------------------------------------------

const sessionEvalPackJSON = `{
	"id": "integration-session-eval-binding",
	"version": "1.0.0",
	"description": "Session-triggered judge eval naming its provider",
	"requires": {
		"providers": [
			{"key": "grader", "role": "llm", "description": "grades the session", "required": true}
		]
	},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"evals": [
				{
					"id": "session_quality",
					"type": "llm_judge_session",
					"trigger": "on_session_complete",
					"params": {"criteria": "was the assistant helpful", "provider": "grader"}
				}
			]
		}
	}
}`

// Session evals run on Close(), off a different context than turn evals. The
// binding has to be attached there too, or a session-scoped check resolves
// nothing while the turn-scoped one beside it works.
func TestProviderBinding_SessionEvalResolvesItsProvider(t *testing.T) {
	packPath := writePackFile(t, sessionEvalPackJSON)
	judgeRepo := &recordingRepo{response: `{"passed": true, "score": 1.0, "reasoning": "helpful"}`}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithNamedProvider(sdk.ProviderSpec{
			ID: "grader", Type: "mock", Model: "mock-model",
			AdditionalConfig: map[string]any{"repository": mock.ResponseRepository(judgeRepo)},
		}),
		sdk.WithEvalRunner(evals.NewEvalRunner(evals.NewEvalTypeRegistry())),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)

	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	require.NoError(t, conv.Close()) // session evals dispatch here

	require.Eventually(t, func() bool { return judgeRepo.count() > 0 }, 2*time.Second, 10*time.Millisecond,
		"the session eval never reached the provider bound to the name its pack chose")
}

// --- the removed param ----------------------------------------------------

// A pack still using classifier_id must be told, not quietly scored against
// whatever the host happened to make default — which is what ignoring an
// unknown param would have done.
func TestProviderBinding_RemovedClassifierIDIsRejected(t *testing.T) {
	pack := strings.Replace(classifyCheckPack("screener"),
		`"provider": "screener"`, `"classifier_id": "screener"`, 1)
	classifier := &stubTextClassifier{}

	results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
		PackData:             []byte(pack),
		PromptName:           "chat",
		SkipSchemaValidation: true,
		ProviderBinding:      testBinding{classifier: classifier},
		Messages: []types.Message{
			{Role: "user", Content: "what a lovely day"},
			{Role: "assistant", Content: "glad to hear it"},
		},
	})

	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].Error, "classifier_id is no longer supported")
	assert.Contains(t, results[0].Error, handlers.ProviderParam,
		"the error must name the replacement")
	assert.Zero(t, classifier.count(),
		"a pack using the removed param must not silently score against some other provider")
}

// --- enforcement, not just resolution -------------------------------------

const blockingGuardrailPackJSON = `{
	"id": "integration-blocking-guardrail",
	"version": "1.0.0",
	"description": "Judge-backed guardrail that blocks on a bad verdict",
	"requires": {
		"providers": [
			{"key": "grader", "role": "llm", "description": "grades toxicity", "required": true}
		]
	},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"validators": [
				{"type": "toxicity", "enabled": true, "message": "Blocked by policy.",
				 "params": {"provider": "grader", "direction": "output"}}
			]
		}
	}
}`

// The point of resolving the provider is that the guardrail can act on what it
// says. A verdict of "toxic" from the provider the host bound has to replace the
// assistant's reply — resolution alone would be a check that runs and decides
// nothing.
func TestProviderBinding_GuardrailActsOnTheBoundProvidersVerdict(t *testing.T) {
	packPath := writePackFile(t, blockingGuardrailPackJSON)
	judgeRepo := &recordingRepo{response: `{"passed": false, "score": 0.0, "reasoning": "toxic"}`}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithNamedProvider(sdk.ProviderSpec{
			ID: "grader", Type: "mock", Model: "mock-model",
			AdditionalConfig: map[string]any{"repository": mock.ResponseRepository(judgeRepo)},
		}),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "say something awful")
	require.NoError(t, err)

	assert.Positive(t, judgeRepo.count(), "the guardrail never consulted the bound provider")
	assert.Contains(t, resp.Text(), "Blocked by policy.",
		"the guardrail resolved its provider but did not act on the verdict")
}

// --- a second name, to prove names are not interchangeable ----------------

// Two providers, two names, one check: it must use the one it asked for. A
// binding that returned "whatever llm is around" would pass every test above
// and fail this one.
func TestProviderBinding_UsesTheNamedProviderNotJustAnyProvider(t *testing.T) {
	packPath := writePackFile(t, blockingGuardrailPackJSON)
	wanted := &recordingRepo{response: `{"passed": true, "score": 1.0, "reasoning": "fine"}`}
	other := &recordingRepo{response: `{"passed": false, "score": 0.0, "reasoning": "toxic"}`}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithNamedProvider(sdk.ProviderSpec{
			ID: "grader", Type: "mock", Model: "mock-model",
			AdditionalConfig: map[string]any{"repository": mock.ResponseRepository(wanted)},
		}),
		sdk.WithNamedProvider(sdk.ProviderSpec{
			ID: "summarizer", Type: "mock", Model: "mock-model",
			AdditionalConfig: map[string]any{"repository": mock.ResponseRepository(other)},
		}),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	assert.Positive(t, wanted.count(), "the check did not use the provider it named")
	assert.Zero(t, other.count(), "the check used a provider it never asked for")
	assert.NotContains(t, resp.Text(), "Blocked by policy.",
		"the clean verdict from the named provider was not the one acted on")
}
