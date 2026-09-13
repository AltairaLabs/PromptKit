package classify_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
)

// stubTopic implements only TopicClassifier, so it also proves RegisterBackend
// registers a backend against the one task it satisfies and no others.
type stubTopic struct{ decision classify.TopicDecision }

func (s *stubTopic) ClassifyTopic(
	_ context.Context, _ classify.TopicRequest,
) (classify.TopicResult, error) {
	return classify.TopicResult{Decision: s.decision, Raw: string(s.decision)}, nil
}

func TestRegisterBackend_RegistersTopicTask(t *testing.T) {
	reg := classify.NewRegistry()

	tasks := classify.RegisterBackend(reg, "topic-control", &stubTopic{decision: classify.TopicAllow})

	assert.Equal(t, []string{"topic"}, tasks,
		"a topic-only backend must claim the topic task and nothing else")

	_, err := reg.TextClassifier("topic-control")
	assert.Error(t, err, "a topic-only backend must not appear as a text classifier")
}

func TestRegistry_TopicClassifier_ResolvesExplicitIDAndDefault(t *testing.T) {
	reg := classify.NewRegistry()
	reg.RegisterTopic("a", &stubTopic{decision: classify.TopicAllow})
	reg.RegisterTopic("b", &stubTopic{decision: classify.TopicDeny})
	require.NoError(t, reg.SetDefaultTopic("b"))

	byID, err := reg.TopicClassifier("a")
	require.NoError(t, err)
	res, err := byID.ClassifyTopic(context.Background(), classify.TopicRequest{})
	require.NoError(t, err)
	assert.Equal(t, classify.TopicAllow, res.Decision)

	byDefault, err := reg.TopicClassifier("")
	require.NoError(t, err)
	res, err = byDefault.ClassifyTopic(context.Background(), classify.TopicRequest{})
	require.NoError(t, err)
	assert.Equal(t, classify.TopicDeny, res.Decision, "empty id must resolve the default")
}

func TestRegistry_TopicClassifier_ErrorsWhenNothingBound(t *testing.T) {
	reg := classify.NewRegistry()

	_, err := reg.TopicClassifier("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no topic classifier")

	_, err = reg.TopicClassifier("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestBuildRegistry_TopicDefaultIsFirstWins(t *testing.T) {
	reg := classify.NewRegistry()
	classify.RegisterBackendDefaulting(reg, "first", &stubTopic{decision: classify.TopicAllow})
	classify.RegisterBackendDefaulting(reg, "second", &stubTopic{decision: classify.TopicDeny})

	c, err := reg.TopicClassifier("")
	require.NoError(t, err)
	res, err := c.ClassifyTopic(context.Background(), classify.TopicRequest{})
	require.NoError(t, err)
	assert.Equal(t, classify.TopicAllow, res.Decision, "first registration wins the default")
}
