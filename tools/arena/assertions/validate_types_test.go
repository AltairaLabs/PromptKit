package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register built-in handlers
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAssertionTypes(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()

	t.Run("valid types produce no errors", func(t *testing.T) {
		scenarios := map[string]*config.Scenario{
			"test-scenario": {
				ConversationAssertions: []config.AssertionConfig{
					{Type: "contains"},
					{Type: "tools_called"},
				},
				Turns: []config.TurnDefinition{
					{
						Assertions: []config.AssertionConfig{
							{Type: "regex"},
							{Type: "max_length"},
						},
					},
				},
			},
		}
		errs := ValidateAssertionTypes(scenarios, registry)
		assert.Empty(t, errs)
	})

	t.Run("aliases are accepted", func(t *testing.T) {
		scenarios := map[string]*config.Scenario{
			"test": {
				ConversationAssertions: []config.AssertionConfig{
					{Type: "tool_called"},     // alias for tools_called
					{Type: "banned_words"},    // alias for content_excludes
					{Type: "content_matches"}, // alias for regex
				},
			},
		}
		errs := ValidateAssertionTypes(scenarios, registry)
		assert.Empty(t, errs)
	})

	t.Run("invalid types produce errors with suggestions", func(t *testing.T) {
		scenarios := map[string]*config.Scenario{
			"hero-scenario": {
				ConversationAssertions: []config.AssertionConfig{
					{Type: "substring_present"},
				},
				Turns: []config.TurnDefinition{
					{
						Assertions: []config.AssertionConfig{
							{Type: "tool_called_with_args"},
						},
					},
				},
			},
		}
		errs := ValidateAssertionTypes(scenarios, registry)
		require.Len(t, errs, 2)
		assert.Contains(t, errs[0], "hero-scenario")
		assert.Contains(t, errs[0], "substring_present")
		assert.Contains(t, errs[1], "hero-scenario")
		assert.Contains(t, errs[1], "tool_called_with_args")
	})

	t.Run("empty scenarios produce no errors", func(t *testing.T) {
		errs := ValidateAssertionTypes(nil, registry)
		assert.Empty(t, errs)
	})

	t.Run("suggestion includes close match", func(t *testing.T) {
		scenarios := map[string]*config.Scenario{
			"test": {
				ConversationAssertions: []config.AssertionConfig{
					{Type: "contain"}, // close to "contains"
				},
			},
		}
		errs := ValidateAssertionTypes(scenarios, registry)
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "contains")
	})
}
