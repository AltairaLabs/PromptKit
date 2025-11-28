package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

func TestLLMJudgeValidator_ConversationAware(t *testing.T) {
	repo := mock.NewInMemoryMockRepository(`{"passed":true,"score":0.5,"reasoning":"ok"}`)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}
	msgs := []types.Message{
		{Role: "user", Content: "Why blue?"},
		{Role: "assistant", Content: "Because it's calm."},
	}
	params := map[string]interface{}{
		"criteria":           "answer makes sense",
		"conversation_aware": true,
		"_metadata": map[string]interface{}{
			"judge_targets": map[string]providers.ProviderSpec{"default": spec},
		},
		"_execution_context_messages": msgs,
	}
	validator := NewLLMJudgeValidator(nil)
	result := validator.Validate("It is calm.", params)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %+v", result.Details)
	}
}

func TestLLMJudgeValidator_NamedJudgeNotFound(t *testing.T) {
	spec := providers.ProviderSpec{ID: "mock-judge", Type: "mock", Model: "judge-model"}
	params := map[string]interface{}{
		"criteria": "be nice",
		"judge":    "missing",
		"_metadata": map[string]interface{}{
			"judge_targets": map[string]providers.ProviderSpec{"default": spec},
		},
	}
	validator := NewLLMJudgeValidator(nil)
	res := validator.Validate("hi", params)
	if res.Passed {
		t.Fatalf("expected fail when named judge missing")
	}
}

func TestParseJudgeVerdictFallback(t *testing.T) {
	verdict := parseJudgeVerdict("passed: true")
	if !verdict.Passed {
		t.Fatalf("expected fallback parse to mark passed")
	}
}

// Ensure the validator is registered in the arena registry
func TestArenaRegistry_RegistersLLMJudge(t *testing.T) {
	reg := NewArenaAssertionRegistry()
	if _, ok := reg.Get("llm_judge"); !ok {
		t.Fatalf("llm_judge not registered in arena registry")
	}
}

func TestBuildJudgeRequest_UsesPromptRegistry(t *testing.T) {
	repo := memory.NewPromptRepository()
	cfg := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       "judge-template",
			Version:        "1.0.0",
			Description:    "test judge",
			SystemTemplate: "SYS {{criteria}} {{response}}",
			Variables: []prompt.VariableMetadata{
				{Name: "criteria", Required: true},
				{Name: "response", Required: true},
			},
		},
	}
	if err := repo.SavePrompt(cfg); err != nil {
		t.Fatalf("failed to save prompt: %v", err)
	}
	reg := prompt.NewRegistryWithRepository(repo)

	params := map[string]interface{}{
		"criteria": "crit",
		"_metadata": map[string]interface{}{
			"prompt_registry": reg,
			"judge_defaults": map[string]interface{}{
				"prompt": "judge-template",
			},
		},
	}

	req := buildJudgeRequest("resp", params, "")
	if req.System != "SYS crit resp" {
		t.Fatalf("expected system prompt from registry, got: %s", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("expected single user message")
	}
}
