package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// A pack names the providers it needs; the host binds those names. Everything
// below is about what happens when the two do not line up — and all of it has
// to happen at Open(), because a conversation that starts and then cannot run
// its safety checks is the failure this whole mechanism exists to prevent.

// classifyCheckPack declares a classifier by logical name and points a check at
// it. `%s` is the name the CHECK uses, so a test can introduce a typo.
func classifyCheckPack(checkProviderName string) string {
	return `{
	"id": "integration-provider-binding",
	"version": "1.0.0",
	"description": "Pack with a classify-backed check",
	"requires": {
		"providers": [
			{"key": "screener", "role": "inference", "description": "scores message sentiment", "required": true}
		]
	},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"evals": [
				{
					"id": "tone",
					"type": "text_sentiment",
					"trigger": "every_turn",
					"params": {"model": "stub-ser", "expected_label": "positive", "provider": "` + checkProviderName + `"}
				}
			]
		}
	}
}`
}

// stubTextClassifier is a classify backend that implements the text task and
// counts the calls, so a test can prove the check reached the backend the host
// bound rather than merely that Open() was happy.
type stubTextClassifier struct {
	mu    sync.Mutex
	calls int
}

func (s *stubTextClassifier) ClassifyText(
	_ context.Context, _ string, _ classify.TextOptions,
) ([]classify.LabelScore, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return []classify.LabelScore{{Label: "positive", Score: 0.9}}, nil
}

func (s *stubTextClassifier) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestProviderBinding_HostBindsWhatThePackNamed(t *testing.T) {
	packPath := writePackFile(t, classifyCheckPack("screener"))
	classifier := &stubTextClassifier{}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithClassifier("screener", classifier),
		sdk.WithEvalRunner(evals.NewEvalRunner(evals.NewEvalTypeRegistry())),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, err = conv.Send(context.Background(), "what a lovely day")
	require.NoError(t, err)

	// Turn evals are dispatched asynchronously; wait for the classifier rather
	// than racing it.
	require.Eventually(t, func() bool { return classifier.count() > 0 }, 2*time.Second, 10*time.Millisecond,
		"the check never reached the classifier the host bound to the name its pack chose")
}

// The check points at a name the pack never declared — a typo, and the pack
// author's to fix. The host cannot bind their way out of it.
func TestProviderBinding_UndeclaredNameFailsAtOpen(t *testing.T) {
	packPath := writePackFile(t, classifyCheckPack("screner"))

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithClassifier("screener", &stubTextClassifier{}),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not declare in requires")
	assert.Contains(t, err.Error(), "screner", "the error should quote the name the check used")
	assert.Contains(t, err.Error(), "screener", "…and what the pack does declare")
}

// The pack is right and the host bound nothing.
func TestProviderBinding_UnboundNameFailsAtOpen(t *testing.T) {
	packPath := writePackFile(t, classifyCheckPack("screener"))

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "screener")
}

// TestProviderBinding_WrongKindFailsAtOpen is the case a host hits by accident:
// they bound SOMETHING to the name, so nothing looks missing, but it cannot do
// what the check needs. Reporting this as an absence would send them looking for
// a provider they already supplied.
func TestProviderBinding_WrongKindFailsAtOpen(t *testing.T) {
	packPath := writePackFile(t, classifyCheckPack("screener"))

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		// An LLM bound to the name a classify-backed check uses.
		sdk.WithLLMProvider(sdk.ProviderSpec{ID: "screener", Type: "mock", Model: "mock-model"}),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "screener")
	assert.Contains(t, err.Error(), "needs a classify provider",
		"the error must say what the check needed, not merely that something is missing")
}

// The mirror image: a classifier bound to the name a judge-backed check uses.
func TestProviderBinding_ClassifierBoundToAJudgeNameFailsAtOpen(t *testing.T) {
	packPath := writePackFile(t, `{
		"id": "integration-judge-wrong-kind",
		"version": "1.0.0",
		"description": "Judge-backed guardrail pointed at a classifier",
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
					{"type": "toxicity", "enabled": true, "params": {"provider": "grader"}}
				]
			}
		}
	}`)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("agent", "mock-model", false)),
		sdk.WithClassifier("grader", &stubTextClassifier{}),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "grader")
	assert.Contains(t, err.Error(), "runs completions",
		"the error must say the check needs an LLM, not that nothing was bound")
}
