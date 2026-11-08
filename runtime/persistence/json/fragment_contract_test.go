package json

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/repotest"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

func TestPromptRepository_FragmentContract(t *testing.T) {
	checkFragmentContract(t, func(t *testing.T, root string) prompt.FragmentRepository {
		return NewJSONPromptRepository(root, nil)
	})
}

// checkFragmentContract runs the contract suite, which fails t on each broken
// clause.
var checkFragmentContract = repotest.RunFragmentRepositoryContract
