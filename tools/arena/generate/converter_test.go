package generate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

func TestConvertSessionToScenario_Conversation(t *testing.T) {
	session := &SessionDetail{
		SessionSummary: SessionSummary{
			ID:        "session-123",
			Timestamp: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
			{Role: "assistant", Content: "I'm doing well!"},
		},
	}

	sc, err := ConvertSessionToScenario(session, ConvertOptions{})
	require.NoError(t, err)

	assert.Equal(t, "promptkit.altairalabs.ai/v1alpha1", sc.APIVersion)
	assert.Equal(t, "Scenario", sc.Kind)
	assert.Equal(t, "session-123", sc.Metadata.Name)
	assert.Equal(t, "conversation", sc.Spec.TaskType)
	assert.Len(t, sc.Spec.Turns, 2)
	assert.Equal(t, "Hello", sc.Spec.Turns[0].Content)
	assert.Equal(t, "How are you?", sc.Spec.Turns[1].Content)
	assert.Empty(t, sc.Spec.Steps)
}

func TestConvertSessionToScenario_Workflow(t *testing.T) {
	session := &SessionDetail{
		SessionSummary: SessionSummary{ID: "wf-session"},
		Messages: []types.Message{
			{Role: "user", Content: "Start order"},
			{Role: "assistant", Content: "Order started"},
			{Role: "user", Content: "Add item"},
		},
	}

	sc, err := ConvertSessionToScenario(session, ConvertOptions{Pack: "order.pack.json"})
	require.NoError(t, err)

	assert.Equal(t, "order.pack.json", sc.Spec.Pack)
	assert.Empty(t, sc.Spec.TaskType)
	assert.Empty(t, sc.Spec.Turns)
	assert.Len(t, sc.Spec.Steps, 2)
	assert.Equal(t, "input", sc.Spec.Steps[0].Type)
	assert.Equal(t, "Start order", sc.Spec.Steps[0].Content)
}

func TestConvertSessionToScenario_WithConversationAssertions(t *testing.T) {
	session := &SessionDetail{
		SessionSummary: SessionSummary{ID: "eval-session", HasFailures: true},
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
			{Role: "assistant", Content: "Response"},
		},
		EvalResults: []assertions.ConversationValidationResult{
			{
				Type:    "content_matches",
				Passed:  false,
				Message: "Content did not match pattern",
				Details: map[string]interface{}{"pattern": "expected.*"},
			},
			{
				Type:    "tools_called",
				Passed:  true,
				Message: "Tools were called correctly",
			},
		},
	}

	sc, err := ConvertSessionToScenario(session, ConvertOptions{})
	require.NoError(t, err)

	// Only failed assertions should be included.
	require.Len(t, sc.Spec.ConversationAssertions, 1)
	assert.Equal(t, "content_matches", sc.Spec.ConversationAssertions[0].Type)
	assert.Equal(t, "Content did not match pattern", sc.Spec.ConversationAssertions[0].Message)
	assert.Equal(t, "expected.*", sc.Spec.ConversationAssertions[0].Params["pattern"])
}

func TestConvertSessionToScenario_WithTurnAssertions(t *testing.T) {
	session := &SessionDetail{
		SessionSummary: SessionSummary{ID: "turn-eval-session"},
		Messages: []types.Message{
			{Role: "user", Content: "Q1"},
			{Role: "assistant", Content: "A1"},
			{Role: "user", Content: "Q2"},
			{Role: "assistant", Content: "A2"},
		},
		TurnEvalResults: map[int][]TurnEvalResult{
			0: {
				{Type: "content_includes", Passed: false, Message: "Missing keyword", Params: map[string]interface{}{"text": "hello"}},
			},
			1: {
				{Type: "content_includes", Passed: true, Message: "OK"},
			},
		},
	}

	sc, err := ConvertSessionToScenario(session, ConvertOptions{})
	require.NoError(t, err)

	// Turn 0 should have the failed assertion.
	require.Len(t, sc.Spec.Turns, 2)
	require.Len(t, sc.Spec.Turns[0].Assertions, 1)
	assert.Equal(t, "content_includes", sc.Spec.Turns[0].Assertions[0].Type)

	// Turn 1 should have no assertions (the one present passed).
	assert.Empty(t, sc.Spec.Turns[1].Assertions)
}

func TestConvertSessionToScenario_NilSession(t *testing.T) {
	_, err := ConvertSessionToScenario(nil, ConvertOptions{})
	require.Error(t, err)
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"Hello World!", "hello-world"},
		{"UPPER_CASE", "upper-case"},
		{"---leading-trailing---", "leading-trailing"},
		{"", "generated"},
		{"a" + string(make([]byte, 100)), "a"},
		{"special@#$chars", "special-chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeID(tt.input)
			assert.Equal(t, tt.expected, got)
			assert.LessOrEqual(t, len(got), maxIDLength)
		})
	}
}

func TestSanitizeID_LongInput(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "abcd"
	}
	got := sanitizeID(long)
	assert.LessOrEqual(t, len(got), maxIDLength)
}
