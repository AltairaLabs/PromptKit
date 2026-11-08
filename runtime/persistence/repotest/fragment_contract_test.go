package repotest_test

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/repotest"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// The suite passes the real repositories: one that honors paths and one that
// refuses them with the sentinel.
func TestContract_PassesRealRepositories(t *testing.T) {
	t.Run("yaml", func(t *testing.T) {
		checkFragmentContract(t, func(_ *testing.T, root string) prompt.FragmentRepository {
			return yaml.NewYAMLPromptRepository(root, nil)
		})
	})
	t.Run("memory", func(t *testing.T) {
		checkFragmentContract(t, func(_ *testing.T, _ string) prompt.FragmentRepository {
			repo := memory.NewPromptRepository()
			repo.RegisterFragment(repotest.NamedFragment, &prompt.Fragment{Content: repotest.NamedContent})
			return repo
		})
	})
}

// fakeRepo answers LoadFragment with fn.
type fakeRepo func(name, path, baseDir string) (*prompt.Fragment, error)

func (f fakeRepo) LoadFragment(name, path, baseDir string) (*prompt.Fragment, error) {
	return f(name, path, baseDir)
}

func named(name string) (*prompt.Fragment, error) {
	if name == repotest.NamedFragment {
		return &prompt.Fragment{Content: repotest.NamedContent}, nil
	}
	return nil, fmt.Errorf("fragment %q: %w", name, fs.ErrNotExist)
}

// Each broken repository must fail the named checks, and pass the others.
func TestContract_CatchesBrokenRepositories(t *testing.T) {
	yamlRepo := func(root string) prompt.FragmentRepository { return yaml.NewYAMLPromptRepository(root, nil) }
	cases := []struct {
		name  string
		repo  func(root string) prompt.FragmentRepository
		fails []string
	}{
		{
			name: "answers every lookup by name, ignoring the path",
			repo: func(string) prompt.FragmentRepository {
				return fakeRepo(func(name, _, _ string) (*prompt.Fragment, error) { return named(name) })
			},
			fails: []string{
				"relative path resolves against baseDir", "absolute path ignores baseDir",
				"path miss is fs.ErrNotExist", "invalid file is not fs.ErrNotExist",
				"unreadable path is not fs.ErrNotExist",
			},
		},
		{
			name: "a miss is a plain error",
			repo: func(root string) prompt.FragmentRepository {
				return fakeRepo(func(name, path, baseDir string) (*prompt.Fragment, error) {
					f, err := yamlRepo(root).LoadFragment(name, path, baseDir)
					if errors.Is(err, fs.ErrNotExist) {
						return nil, errors.New("not found")
					}
					return f, err
				})
			},
			fails: []string{"name-only miss is fs.ErrNotExist", "path miss is fs.ErrNotExist"},
		},
		{
			name: "every failure is fs.ErrNotExist",
			repo: func(root string) prompt.FragmentRepository {
				return fakeRepo(func(name, path, baseDir string) (*prompt.Fragment, error) {
					f, err := yamlRepo(root).LoadFragment(name, path, baseDir)
					if err != nil {
						return nil, fmt.Errorf("%w: %v", fs.ErrNotExist, err)
					}
					return f, nil
				})
			},
			fails: []string{"invalid file is not fs.ErrNotExist", "unreadable path is not fs.ErrNotExist"},
		},
		{
			name: "joins an absolute path onto baseDir",
			repo: func(root string) prompt.FragmentRepository {
				return fakeRepo(func(name, path, baseDir string) (*prompt.Fragment, error) {
					if filepath.IsAbs(path) {
						path = filepath.Join(baseDir, path)
					}
					return yamlRepo(root).LoadFragment(name, path, baseDir)
				})
			},
			fails: []string{"absolute path ignores baseDir"},
		},
		{
			name: "path sentinel also satisfies fs.ErrNotExist",
			repo: func(string) prompt.FragmentRepository {
				return fakeRepo(func(name, path, _ string) (*prompt.Fragment, error) {
					if path != "" {
						return nil, fmt.Errorf("%w: %w", persistence.ErrFragmentPathUnsupported, fs.ErrNotExist)
					}
					return named(name)
				})
			},
			fails: []string{
				"relative path resolves against baseDir", "absolute path ignores baseDir",
				"path miss is fs.ErrNotExist", "invalid file is not fs.ErrNotExist",
				"unreadable path is not fs.ErrNotExist",
			},
		},
		{
			name: "returns neither a fragment nor an error",
			repo: func(string) prompt.FragmentRepository {
				return fakeRepo(func(string, string, string) (*prompt.Fragment, error) { return nil, nil })
			},
			fails: []string{
				"name-only lookup finds the fragment", "name-only miss is fs.ErrNotExist",
				"relative path resolves against baseDir", "absolute path ignores baseDir",
				"path miss is fs.ErrNotExist", "invalid file is not fs.ErrNotExist",
				"unreadable path is not fs.ErrNotExist",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := make(map[string]bool, len(tc.fails))
			for _, name := range tc.fails {
				want[name] = true
			}
			for _, check := range repotest.Checks() {
				root := t.TempDir()
				if err := repotest.WriteFixtures(root); err != nil {
					t.Fatal(err)
				}
				err := check.Run(tc.repo(root), root)
				if want[check.Name] && err == nil {
					t.Errorf("%s: check passed, want it to catch the repository", check.Name)
				}
				if !want[check.Name] && err != nil {
					t.Errorf("%s: check failed: %v", check.Name, err)
				}
			}
		})
	}
}

// checkFragmentContract runs the contract suite, which fails t on each broken
// clause.
var checkFragmentContract = repotest.RunFragmentRepositoryContract
