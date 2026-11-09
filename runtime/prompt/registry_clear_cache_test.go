package prompt_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// ClearCache makes the registry read the repository again: a prompt and a
// fragment changed in the repository after the first load are served from
// the cache until ClearCache, and fresh afterwards.
func TestRegistry_ClearCache_ReloadsFromTheRepository(t *testing.T) {
	repo := memory.NewPromptRepository()
	cfg := func(system string) *prompt.Config {
		return &prompt.Config{Spec: prompt.Spec{
			TaskType:       "support",
			SystemTemplate: system + " {{tone}}",
			Fragments:      []prompt.FragmentRef{{Name: "tone"}},
		}}
	}
	repo.RegisterPrompt("support", cfg("v1"))
	repo.RegisterFragment("tone", &prompt.Fragment{Content: "calm"})
	reg := prompt.NewRegistryWithRepository(repo)

	first := reg.Load("support")
	require.NotNil(t, first)
	assert.Equal(t, "v1 calm", first.SystemPrompt)

	repo.RegisterPrompt("support", cfg("v2"))
	repo.RegisterFragment("tone", &prompt.Fragment{Content: "brisk"})
	cached := reg.Load("support")
	require.NotNil(t, cached)
	assert.Equal(t, "v1 calm", cached.SystemPrompt, "before ClearCache the cached prompt and fragment are served")

	reg.ClearCache()
	fresh := reg.Load("support")
	require.NotNil(t, fresh)
	assert.Equal(t, "v2 brisk", fresh.SystemPrompt)
}
