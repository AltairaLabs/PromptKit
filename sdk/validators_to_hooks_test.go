package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

func TestConvertPackValidatorsToHooks(t *testing.T) {
	t.Run("no validators is no-op", func(t *testing.T) {
		prompt := &pack.Prompt{}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		assert.Empty(t, cfg.providerHooks)
	})

	t.Run("converts known validator to hook", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{Type: "banned_words", Config: map[string]any{"words": []any{"bad"}}},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 1)
		assert.Equal(t, "banned_words", cfg.providerHooks[0].Name())
	})

	t.Run("skips unknown validator type", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{Type: "nonexistent", Config: map[string]any{}},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		assert.Empty(t, cfg.providerHooks)
	})

	t.Run("pack validators prepended before user hooks", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{Type: "banned_words", Config: map[string]any{"words": []any{"bad"}}},
			},
		}
		userHook := &testProviderHook{name: "user-hook"}
		cfg := &config{
			providerHooks: []hooks.ProviderHook{userHook},
		}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 2)
		assert.Equal(t, "banned_words", cfg.providerHooks[0].Name())
		assert.Equal(t, "user-hook", cfg.providerHooks[1].Name())
	})

	t.Run("multiple validators", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{Type: "banned_words", Config: map[string]any{"words": []any{"bad"}}},
				{Type: "length", Config: map[string]any{"max_characters": 100}},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 2)
		assert.Equal(t, "banned_words", cfg.providerHooks[0].Name())
		assert.Equal(t, "length", cfg.providerHooks[1].Name())
	})
}
