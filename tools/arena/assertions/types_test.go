package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/stretchr/testify/assert"
)

func TestAssertionResult(t *testing.T) {
	ar := AssertionResult{
		Passed:  true,
		Details: map[string]interface{}{"key": "value"},
		Message: "test message",
	}
	assert.True(t, ar.Passed)
	assert.Equal(t, "test message", ar.Message)
	assert.Equal(t, map[string]interface{}{"key": "value"}, ar.Details)
}

func TestAssertionConfig_ToEvalDef_Basic(t *testing.T) {
	cfg := AssertionConfig{
		Type:    "content_includes",
		Params:  map[string]interface{}{"patterns": []string{"hello"}},
		Message: "should include hello",
	}

	def := cfg.ToEvalDef(0)

	assert.Equal(t, "assertion_0_content_includes", def.ID)
	assert.Equal(t, "content_includes", def.Type)
	assert.Equal(t, evals.TriggerEveryTurn, def.Trigger)
	assert.Equal(t, cfg.Params, def.Params)
	assert.Equal(t, "should include hello", def.Message)
	assert.NotNil(t, def.Threshold)
	assert.NotNil(t, def.Threshold.Passed)
	assert.True(t, *def.Threshold.Passed)
	assert.Nil(t, def.When)
}

func TestAssertionConfig_ToEvalDef_WithWhen(t *testing.T) {
	cfg := AssertionConfig{
		Type:    "tools_called",
		Params:  map[string]interface{}{"tools": []string{"search"}},
		Message: "search should be called",
		When: &AssertionWhen{
			ToolCalled:        "search",
			ToolCalledPattern: "search_.*",
			AnyToolCalled:     true,
			MinToolCalls:      2,
		},
	}

	def := cfg.ToEvalDef(3)

	assert.Equal(t, "assertion_3_tools_called", def.ID)
	assert.NotNil(t, def.When)
	assert.Equal(t, "search", def.When.ToolCalled)
	assert.Equal(t, "search_.*", def.When.ToolCalledPattern)
	assert.True(t, def.When.AnyToolCalled)
	assert.Equal(t, 2, def.When.MinToolCalls)
}

func TestAssertionConfig_ToEvalDef_IndexInID(t *testing.T) {
	cfg := AssertionConfig{
		Type:   "content_matches",
		Params: map[string]interface{}{},
	}

	def5 := cfg.ToEvalDef(5)
	def10 := cfg.ToEvalDef(10)

	assert.Equal(t, "assertion_5_content_matches", def5.ID)
	assert.Equal(t, "assertion_10_content_matches", def10.ID)
}

func TestAssertionConfig_ToConversationEvalDef(t *testing.T) {
	cfg := AssertionConfig{
		Type:    "turn_count",
		Params:  map[string]interface{}{"min": 3},
		Message: "should have at least 3 turns",
	}

	def := cfg.ToConversationEvalDef(2)

	assert.Equal(t, "assertion_2_turn_count", def.ID)
	assert.Equal(t, "turn_count", def.Type)
	assert.Equal(t, evals.TriggerOnConversationComplete, def.Trigger)
	assert.Equal(t, cfg.Params, def.Params)
}

func TestAssertionConfig_ToConversationEvalDef_WithWhen(t *testing.T) {
	cfg := AssertionConfig{
		Type: "tools_called",
		When: &AssertionWhen{
			ToolCalled: "search",
		},
	}

	def := cfg.ToConversationEvalDef(0)

	assert.Equal(t, evals.TriggerOnConversationComplete, def.Trigger)
	assert.NotNil(t, def.When)
	assert.Equal(t, "search", def.When.ToolCalled)
}
