package assertions

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestLLMJudgeConversationValidator_Pass(t *testing.T) {
	repo := mock.NewInMemoryMockRepository(`{"passed":true,"score":0.8,"reasoning":"ok"}`)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "Hi"},
			{Role: "assistant", Content: "Hello!"},
		},
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"judge_targets": map[string]providers.ProviderSpec{"default": spec},
			},
		},
	}
	v := NewLLMJudgeConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, map[string]interface{}{
		"criteria": "be polite",
	})
	if !res.Passed {
		t.Fatalf("expected pass, got: %+v", res)
	}
}

func TestLLMJudgeConversationValidator_MissingTargets(t *testing.T) {
	conv := &ConversationContext{AllTurns: []types.Message{{Role: "assistant", Content: "hi"}}, Metadata: ConversationMetadata{}}
	v := NewLLMJudgeConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, map[string]interface{}{"criteria": "be nice"})
	if res.Passed {
		t.Fatalf("expected fail without judge targets")
	}
}

func TestLLMJudgeConversationValidator_MinScore(t *testing.T) {
	repo := mock.NewInMemoryMockRepository(`{"passed":true,"score":0.5,"reasoning":"ok"}`)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: "hello"},
		},
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"judge_targets": map[string]providers.ProviderSpec{"default": spec},
			},
		},
	}
	v := NewLLMJudgeConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, map[string]interface{}{
		"criteria":  "be polite",
		"min_score": 0.8,
	})
	if res.Passed {
		t.Fatalf("expected fail due to min_score, got pass")
	}
}
