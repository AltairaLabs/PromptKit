package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

func TestLLMJudgeValidator_BasicPass(t *testing.T) {
	// Mock judge provider that returns a pass verdict
	repo := mock.NewInMemoryMockRepository(`{"passed":true,"score":0.9,"reasoning":"ok"}`)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}
	params := map[string]interface{}{
		"criteria": "be polite",
		"_metadata": map[string]interface{}{
			"judge_targets": map[string]providers.ProviderSpec{"default": spec},
		},
	}

	validator := NewLLMJudgeValidator(nil)
	result := validator.Validate("Thanks for your question!", params)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %+v", result.Details)
	}
}

func TestLLMJudgeValidator_MissingJudgeTargets(t *testing.T) {
	validator := NewLLMJudgeValidator(nil)
	res := validator.Validate("hi", map[string]interface{}{"criteria": "be nice"})
	if res.Passed {
		t.Fatalf("expected failure when judge targets missing")
	}
}

// Ensure the validator is registered in the arena registry
func TestArenaRegistry_RegistersLLMJudge(t *testing.T) {
	reg := NewArenaAssertionRegistry()
	if _, ok := reg.Get("llm_judge"); !ok {
		t.Fatalf("llm_judge not registered in arena registry")
	}
}
