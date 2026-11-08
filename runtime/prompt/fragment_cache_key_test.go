package prompt

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyedRepo serves a fragment by name, and by path only under the empty base
// dir (the working-directory fallback).
type keyedRepo struct {
	byName, byFallbackPath map[string]string
}

func (r keyedRepo) LoadFragment(name, path, baseDir string) (*Fragment, error) {
	if path == "" {
		if c, ok := r.byName[name]; ok {
			return &Fragment{Content: c}, nil
		}
	} else if baseDir == "" {
		if c, ok := r.byFallbackPath[path]; ok {
			return &Fragment{Content: c}, nil
		}
	}
	return nil, fmt.Errorf("no fragment: %w", fs.ErrNotExist)
}

// A name-only key and an equal working-directory path are two fragments: one
// must never be served from the other's cache entry, in either order.
func TestFragmentCache_NameAndFallbackPathDoNotCollide(t *testing.T) {
	repo := keyedRepo{
		byName:         map[string]string{"support/formal": "BY NAME"},
		byFallbackPath: map[string]string{"support/formal": "BY PATH"},
	}
	byName := FragmentRef{Name: "support/formal", Required: true}
	byPath := FragmentRef{Name: "formal", Path: "support/formal", Required: true}

	for _, order := range [][]FragmentRef{{byName, byPath}, {byPath, byName}} {
		fr := NewFragmentResolverWithRepository(repo)
		got := map[string]string{}
		for _, ref := range order {
			vars, err := fr.AssembleFragments([]FragmentRef{ref}, nil, "/prompts/dir")
			require.NoError(t, err)
			for k, v := range vars {
				got[k] = v
			}
		}
		assert.Equal(t, "BY NAME", got["support/formal"])
		assert.Equal(t, "BY PATH", got["formal"])
	}
}
