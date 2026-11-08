package memory

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/repotest"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// The in-memory repository holds fragments by name, so it must answer a
// name-only lookup and refuse a path with ErrFragmentPathUnsupported.
func TestPromptRepository_FragmentContract(t *testing.T) {
	checkFragmentContract(t, func(t *testing.T, _ string) prompt.FragmentRepository {
		repo := NewPromptRepository()
		repo.RegisterFragment(repotest.NamedFragment, &prompt.Fragment{Content: repotest.NamedContent})
		return repo
	})
}

// checkFragmentContract runs the contract suite, which fails t on each broken
// clause.
var checkFragmentContract = repotest.RunFragmentRepositoryContract
