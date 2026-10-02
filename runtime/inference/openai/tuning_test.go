package openai_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

func TestFactory_RequestTimeoutFromTuning(t *testing.T) {
	p, err := inference.CreateFromSpec(inference.ProviderSpec{
		Type: "openai", Model: "gpt-test",
		Tuning: base.HTTPTuning{RequestTimeout: 7 * time.Second},
	})
	require.NoError(t, err)
	provider, ok := p.(*openai.Provider)
	require.True(t, ok)
	assert.Equal(t, 7*time.Second, provider.HTTPTimeout())
}

func TestFactory_RejectsTimeoutSecondsWithRequestTimeout(t *testing.T) {
	for _, typ := range []string{"openai", "nvidia-topic-control"} {
		t.Run(typ, func(t *testing.T) {
			_, err := inference.CreateFromSpec(inference.ProviderSpec{
				Type: typ, Model: "gpt-test", BaseURL: "http://127.0.0.1:1",
				AdditionalConfig: map[string]any{"timeout_seconds": 10},
				Tuning:           base.HTTPTuning{RequestTimeout: 7 * time.Second},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "timeout_seconds or request_timeout, not both")
		})
	}
}
