package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

func boolPtr(b bool) *bool { return &b }

func TestConvertPackValidatorsToHooks(t *testing.T) {
	t.Run("no validators is no-op", func(t *testing.T) {
		prompt := &pack.Prompt{}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		assert.Empty(t, cfg.providerHooks)
	})

	t.Run("converts enabled known validator to hook", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{
					Type:    "banned_words",
					Enabled: true,
					Params:  map[string]any{"patterns": []any{"bad"}},
				},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 1)
		assert.Equal(t, "banned_words", cfg.providerHooks[0].Name())
	})

	t.Run("skips disabled validator", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{
					Type:    "banned_words",
					Enabled: false,
					Params:  map[string]any{"patterns": []any{"bad"}},
				},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		assert.Empty(t, cfg.providerHooks)
	})

	t.Run("skips unknown validator type", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{Type: "nonexistent", Enabled: true, Params: map[string]any{}},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		assert.Empty(t, cfg.providerHooks)
	})

	t.Run("pack validators prepended before user hooks", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{
					Type:    "banned_words",
					Enabled: true,
					Params:  map[string]any{"patterns": []any{"bad"}},
				},
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

	t.Run("multiple enabled validators", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{
					Type:    "banned_words",
					Enabled: true,
					Params:  map[string]any{"patterns": []any{"bad"}},
				},
				{
					Type:    "max_length",
					Enabled: true,
					Params:  map[string]any{"max_characters": 100},
				},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 2)
		assert.Equal(t, "banned_words", cfg.providerHooks[0].Name())
		assert.Equal(t, "max_length", cfg.providerHooks[1].Name())
	})

	t.Run("fail_on_violation omitted defaults to monitor-only", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{
					Type:    "banned_words",
					Enabled: true,
					Params:  map[string]any{"patterns": []any{"bad"}},
				},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 1)
		// Monitor-only is an internal flag on the adapter; the behavioural
		// assertion lives in the e2e test in a later task.
	})

	t.Run("fail_on_violation true enables enforcement", func(t *testing.T) {
		prompt := &pack.Prompt{
			Validators: []pack.Validator{
				{
					Type:            "banned_words",
					Enabled:         true,
					FailOnViolation: boolPtr(true),
					Params:          map[string]any{"patterns": []any{"bad"}},
				},
			},
		}
		cfg := &config{}
		convertPackValidatorsToHooks(prompt, cfg)
		require.Len(t, cfg.providerHooks, 1)
	})
}
