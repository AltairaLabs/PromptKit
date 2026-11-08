// Package repotest is a contract suite for prompt.FragmentRepository
// implementations. It is an ordinary package, not a test file, so a
// repository written outside PromptKit can run the same checks as the ones in
// runtime/persistence:
//
//	func TestFragmentContract(t *testing.T) {
//		repotest.RunFragmentRepositoryContract(t, func(t *testing.T, root string) prompt.FragmentRepository {
//			return myrepo.New(root)
//		})
//	}
package repotest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// The fixture a name-only lookup must find. The suite writes it as
// <root>/fragments/contract_named.yaml and <root>/fragments/contract_named.json
// before it builds the repository. A repository that does not read fragments
// from files must hold it some other way; the in-memory one registers it:
//
//	repo.RegisterFragment(repotest.NamedFragment, &prompt.Fragment{Content: repotest.NamedContent})
const (
	NamedFragment = "contract_named"
	NamedContent  = "Fragment found by name."
)

// Fixtures the path checks load. Each path check asks for NamedFragment, so a
// repository that ignores the path and answers by name returns NamedContent
// and fails the check.
const (
	underA       = "Fragment under a."
	underB       = "Fragment under b."
	atAbsolute   = "Fragment at an absolute path."
	fragmentFile = "frag.yaml"
	fileMode     = 0o600
	dirMode      = 0o750
)

// fixtureContent is a fragment file's text. It is JSON, which is also YAML, so
// the YAML and JSON repositories read the same files.
func fixtureContent(content string) string {
	return fmt.Sprintf(`{"fragment_type": "text", "version": "v1", "content": %q}`, content)
}

// Check is one clause of the contract. It returns an error describing how the
// repository broke it, or nil.
type Check struct {
	Name string
	Run  func(repo prompt.FragmentRepository, root string) error
}

// RunFragmentRepositoryContract checks a FragmentRepository against the
// contract FragmentResolver relies on:
//
//   - a lookup with no path finds the fragment stored under that name;
//   - a relative path resolves against baseDir, so one relative path under two
//     base directories is two fragments;
//   - an absolute path loads that file and ignores baseDir;
//   - a fragment that does not exist is an error satisfying
//     errors.Is(err, fs.ErrNotExist). FragmentResolver falls back from the
//     prompt's directory to the working directory only on that error;
//   - a file that exists but cannot be read or parsed is an error that does
//     not satisfy fs.ErrNotExist, so the resolver reports it rather than
//     looking elsewhere;
//   - a repository that cannot honor a path (the in-memory one) says so with
//     persistence.ErrFragmentPathUnsupported on every path lookup, and never
//     answers one by name.
//
// newRepo builds the repository over root after the suite has written its
// fixtures there.
func RunFragmentRepositoryContract(
	t *testing.T, newRepo func(t *testing.T, root string) prompt.FragmentRepository,
) {
	t.Helper()
	for _, check := range Checks() {
		t.Run(check.Name, func(t *testing.T) {
			root := t.TempDir()
			if err := WriteFixtures(root); err != nil {
				t.Fatalf("writing fixtures: %v", err)
			}
			if err := check.Run(newRepo(t, root), root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// WriteFixtures writes the files the checks load under root.
func WriteFixtures(root string) error {
	files := map[string]string{
		filepath.Join("fragments", NamedFragment+".yaml"): fixtureContent(NamedContent),
		filepath.Join("fragments", NamedFragment+".json"): fixtureContent(NamedContent),
		filepath.Join("a", fragmentFile):                  fixtureContent(underA),
		filepath.Join("b", fragmentFile):                  fixtureContent(underB),
		filepath.Join("abs", fragmentFile):                fixtureContent(atAbsolute),
		filepath.Join("bad", fragmentFile):                `{"content": "unterminated`,
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), fileMode); err != nil {
			return err
		}
	}
	// A path that exists but is a directory: it cannot be read as a file.
	return os.MkdirAll(filepath.Join(root, "dir", fragmentFile), dirMode)
}

// Checks returns the contract's checks, in the order the suite runs them.
func Checks() []Check {
	return []Check{
		{Name: "name-only lookup finds the fragment", Run: checkNameOnly},
		{Name: "name-only miss is fs.ErrNotExist", Run: checkNameOnlyMissing},
		{Name: "relative path resolves against baseDir", Run: checkRelativePath},
		{Name: "absolute path ignores baseDir", Run: checkAbsolutePath},
		{Name: "path miss is fs.ErrNotExist", Run: checkPathMissing},
		{Name: "invalid file is not fs.ErrNotExist", Run: checkInvalidFile},
		{Name: "unreadable path is not fs.ErrNotExist", Run: checkUnreadablePath},
	}
}

func checkNameOnly(repo prompt.FragmentRepository, _ string) error {
	return wantContent(repo, NamedFragment, "", "", NamedContent)
}

func checkNameOnlyMissing(repo prompt.FragmentRepository, _ string) error {
	return wantNotExist(repo, "contract_missing", "", "")
}

func checkRelativePath(repo prompt.FragmentRepository, root string) error {
	cases := []struct{ path, baseDir, want string }{
		{fragmentFile, filepath.Join(root, "a"), underA},
		{fragmentFile, filepath.Join(root, "b"), underB},
		{filepath.Join("..", fragmentFile), filepath.Join(root, "a", "prompts"), underA},
	}
	for _, c := range cases {
		if err := wantPathContent(repo, c.path, c.baseDir, c.want); err != nil {
			return err
		}
	}
	return nil
}

func checkAbsolutePath(repo prompt.FragmentRepository, root string) error {
	return wantPathContent(repo, filepath.Join(root, "abs", fragmentFile), filepath.Join(root, "a"), atAbsolute)
}

func checkPathMissing(repo prompt.FragmentRepository, root string) error {
	return wantPathFailure(repo, "missing.yaml", filepath.Join(root, "a"), true)
}

func checkInvalidFile(repo prompt.FragmentRepository, root string) error {
	return wantPathFailure(repo, fragmentFile, filepath.Join(root, "bad"), false)
}

func checkUnreadablePath(repo prompt.FragmentRepository, root string) error {
	return wantPathFailure(repo, fragmentFile, filepath.Join(root, "dir"), false)
}

// lookup describes one LoadFragment call, for error messages.
func lookup(name, path, baseDir string) string {
	return fmt.Sprintf("LoadFragment(%q, %q, %q)", name, path, baseDir)
}

func wantContent(repo prompt.FragmentRepository, name, path, baseDir, want string) error {
	frag, err := repo.LoadFragment(name, path, baseDir)
	if err != nil {
		return fmt.Errorf("%s: %w", lookup(name, path, baseDir), err)
	}
	if frag == nil {
		return fmt.Errorf("%s returned no fragment and no error", lookup(name, path, baseDir))
	}
	if frag.Content != want {
		return fmt.Errorf("%s content = %q, want %q", lookup(name, path, baseDir), frag.Content, want)
	}
	return nil
}

func wantNotExist(repo prompt.FragmentRepository, name, path, baseDir string) error {
	_, err := repo.LoadFragment(name, path, baseDir)
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s error = %v, want one satisfying errors.Is(err, fs.ErrNotExist)",
			lookup(name, path, baseDir), err)
	}
	return nil
}

// pathUnsupported reports whether err is the documented answer of a
// repository that cannot honor a path, and that it is not also fs.ErrNotExist,
// which would send the resolver looking elsewhere.
func pathUnsupported(err error, desc string) (bool, error) {
	if !errors.Is(err, persistence.ErrFragmentPathUnsupported) {
		return false, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true, fmt.Errorf("%s: ErrFragmentPathUnsupported must not satisfy fs.ErrNotExist: %w", desc, err)
	}
	return true, nil
}

// wantPathContent requires a path lookup of NamedFragment to return want, or
// the repository to say it cannot honor a path.
func wantPathContent(repo prompt.FragmentRepository, path, baseDir, want string) error {
	desc := lookup(NamedFragment, path, baseDir)
	frag, err := repo.LoadFragment(NamedFragment, path, baseDir)
	if unsupported, uerr := pathUnsupported(err, desc); unsupported {
		return uerr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", desc, err)
	}
	if frag == nil {
		return fmt.Errorf("%s returned no fragment and no error", desc)
	}
	if frag.Content == NamedContent {
		return fmt.Errorf("%s answered by name and ignored the path; a repository that cannot honor "+
			"a path must return persistence.ErrFragmentPathUnsupported", desc)
	}
	if frag.Content != want {
		return fmt.Errorf("%s content = %q, want %q", desc, frag.Content, want)
	}
	return nil
}

// wantPathFailure requires a path lookup of NamedFragment to fail: with an
// error satisfying fs.ErrNotExist when notExist, otherwise with one that does
// not. ErrFragmentPathUnsupported is accepted in either case.
func wantPathFailure(repo prompt.FragmentRepository, path, baseDir string, notExist bool) error {
	desc := lookup(NamedFragment, path, baseDir)
	frag, err := repo.LoadFragment(NamedFragment, path, baseDir)
	if unsupported, uerr := pathUnsupported(err, desc); unsupported {
		return uerr
	}
	if err == nil {
		content := ""
		if frag != nil {
			content = frag.Content
		}
		return fmt.Errorf("%s succeeded with content %q, want an error", desc, content)
	}
	if got := errors.Is(err, fs.ErrNotExist); got != notExist {
		if notExist {
			return fmt.Errorf("%s error = %v, want one satisfying errors.Is(err, fs.ErrNotExist)", desc, err)
		}
		return fmt.Errorf("%s error = %v satisfies fs.ErrNotExist, so the resolver would look past a real "+
			"failure; it must not", desc, err)
	}
	return nil
}
