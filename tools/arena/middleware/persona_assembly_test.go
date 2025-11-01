package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonaAssemblyMiddleware_BasicPersona(t *testing.T) {
	// Create a simple persona with plain system prompt (no templates)
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		Description:  "A test persona",
		SystemPrompt: "You are a helpful test user.",
		Goals:        []string{"Be helpful", "Ask questions"},
		Constraints:  []string{"Be respectful"},
		Style: config.PersonaStyle{
			Verbosity:      "medium",
			ChallengeLevel: "low",
			FrictionTags:   []string{"polite"},
		},
		Defaults: config.PersonaDefaults{
			Temperature: 0.7,
		},
	}

	baseVars := map[string]string{
		"context_slot": "test context",
		"domain_hint":  "customer support",
	}

	middleware := PersonaAssemblyMiddleware(persona, "us", baseVars)

	ctx := &pipeline.ExecutionContext{
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	called := false
	next := func() error {
		called = true
		return nil
	}

	err := middleware.Process(ctx, next)
	require.NoError(t, err)
	assert.True(t, called, "next() should be called")

	// Check that system prompt was set
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.Contains(t, ctx.SystemPrompt, "helpful test user")

	// Check that base variables were set
	assert.Equal(t, "test context", ctx.Variables["context_slot"])
	assert.Equal(t, "customer support", ctx.Variables["domain_hint"])
}

func TestPersonaAssemblyMiddleware_WithTemplateVariables(t *testing.T) {
	// Create persona that uses BuildSystemPrompt (which injects persona variables)
	persona := &config.UserPersonaPack{
		ID:             "templated-persona",
		Description:    "A persona with templates",
		SystemTemplate: "You are {{persona_role}}. Goals: {{persona_goals}}. Style: {{verbosity}}",
		Goals:          []string{"Test goal"},
		Constraints:    []string{"Test constraint"},
		Style: config.PersonaStyle{
			Verbosity:      "short",
			ChallengeLevel: "high",
		},
		OptionalVars: map[string]string{
			"persona_role": "a tester",
		},
		Defaults: config.PersonaDefaults{
			Temperature: 0.5,
		},
	}

	baseVars := map[string]string{
		"extra_var": "extra value",
	}

	middleware := PersonaAssemblyMiddleware(persona, "us", baseVars)

	ctx := &pipeline.ExecutionContext{
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Check that persona variables were injected
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.Contains(t, ctx.SystemPrompt, "tester")
	assert.Contains(t, ctx.SystemPrompt, "Test goal")

	// Check that base variables are accessible for TemplateMiddleware
	assert.Equal(t, "extra value", ctx.Variables["extra_var"])
}

func TestPersonaAssemblyMiddleware_PreservesExistingVariables(t *testing.T) {
	persona := &config.UserPersonaPack{
		ID:           "preserve-test",
		SystemPrompt: "Test prompt",
		Defaults: config.PersonaDefaults{
			Temperature: 0.7,
		},
	}

	baseVars := map[string]string{
		"var1": "value1",
		"var2": "value2",
	}

	middleware := PersonaAssemblyMiddleware(persona, "us", baseVars)

	ctx := &pipeline.ExecutionContext{
		Variables: map[string]string{
			"var2":     "existing_value2", // Should not be overwritten
			"existing": "should_remain",
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Check that existing variables are preserved
	assert.Equal(t, "existing_value2", ctx.Variables["var2"], "existing var2 should not be overwritten")
	assert.Equal(t, "should_remain", ctx.Variables["existing"])
	assert.Equal(t, "value1", ctx.Variables["var1"], "new var1 should be added")
}

func TestPersonaAssemblyMiddleware_StreamChunk(t *testing.T) {
	persona := &config.UserPersonaPack{
		ID:           "stream-test",
		SystemPrompt: "Test",
		Defaults: config.PersonaDefaults{
			Temperature: 0.7,
		},
	}

	middleware := PersonaAssemblyMiddleware(persona, "us", nil)

	ctx := &pipeline.ExecutionContext{}
	chunk := &providers.StreamChunk{}

	// StreamChunk should be a no-op and not return an error
	err := middleware.StreamChunk(ctx, chunk)
	assert.NoError(t, err)
}

func TestPersonaAssemblyMiddleware_ErrorHandling(t *testing.T) {
	// Create a persona that will fail BuildSystemPrompt (missing required vars)
	persona := &config.UserPersonaPack{
		ID:             "error-persona",
		SystemTemplate: "Template with {{required_missing}}",
		RequiredVars:   []string{"required_missing"},
		Defaults: config.PersonaDefaults{
			Temperature: 0.7,
		},
	}

	middleware := PersonaAssemblyMiddleware(persona, "us", map[string]string{})

	ctx := &pipeline.ExecutionContext{
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	err := middleware.Process(ctx, func() error { return nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to assemble persona prompt")
}
