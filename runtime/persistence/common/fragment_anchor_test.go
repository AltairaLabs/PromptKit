package common_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/common"
	jsonrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/json"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// fileRepos are the repositories that read fragments from files.
var fileRepos = []struct {
	name, ext string
	open      func(basePath string) prompt.FragmentRepository
}{
	{"yaml", ".yaml", func(b string) prompt.FragmentRepository { return yaml.NewYAMLPromptRepository(b, nil) }},
	{"json", ".json", func(b string) prompt.FragmentRepository { return jsonrepo.NewJSONPromptRepository(b, nil) }},
}

// fragmentDoc is a fragment file's text. It is JSON, which is also YAML.
func fragmentDoc(content string) string {
	return fmt.Sprintf(`{"fragment_type": "text", "version": "v1", "content": %q}`, content)
}

func writeFragment(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fragmentDoc(content)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink: %v", err)
	}
}

const (
	insideContent  = "inside the pack"
	outsideContent = "SECRET outside the pack"
)

// swapFixture is a pack at <top>/site/pack, and a look-alike tree at
// <outside>/site/pack whose fragments hold outsideContent. swap replaces the
// pack's ancestor <top>/site with a symlink to <outside>/site, as someone who
// can write in <top> could do while a lookup runs.
func swapFixture(t *testing.T, ext string) (pack string, swap func()) {
	t.Helper()
	top, outside := t.TempDir(), t.TempDir()
	pack = filepath.Join(top, "site", "pack")
	writeFragment(t, filepath.Join(pack, "fragments", "tone"+ext), insideContent)
	writeFragment(t, filepath.Join(outside, "site", "pack", "fragments", "tone"+ext), outsideContent)
	for _, dir := range []string{filepath.Join(pack, "prompts"), filepath.Join(outside, "site", "pack", "prompts")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	symlink(t, outside, filepath.Join(t.TempDir(), "probe")) // skip where there are no symlinks
	swap = func() {
		if err := os.Rename(filepath.Join(top, "site"), filepath.Join(top, "site-moved")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "site"), filepath.Join(top, "site")); err != nil {
			t.Fatal(err)
		}
	}
	return pack, swap
}

// A symlink swapped into the anchor's ancestry after the lookup has resolved
// the anchor does not move the read: the lookup reads from the tree it
// resolved, never from where the new link points.
func TestFileRepositories_AncestorSwappedMidLookupStaysInTheResolvedTree(t *testing.T) {
	cases := []struct {
		name    string
		path    func(ext string) string
		baseDir func(pack string) string
	}{
		{
			name:    "relative path climbing to the pack",
			path:    func(ext string) string { return "../fragments/tone" + ext },
			baseDir: func(pack string) string { return filepath.Join(pack, "prompts") },
		},
		{
			name:    "relative path below its base",
			path:    func(ext string) string { return "fragments/tone" + ext },
			baseDir: func(pack string) string { return pack },
		},
		{
			name:    "name-only lookup",
			path:    func(string) string { return "" },
			baseDir: func(string) string { return "" },
		},
	}
	for _, repo := range fileRepos {
		for _, tc := range cases {
			t.Run(repo.name+"/"+tc.name, func(t *testing.T) {
				pack, swap := swapFixture(t, repo.ext)
				r := repo.open(pack)
				baseDir := tc.baseDir(pack)
				common.SetAfterContainmentCheck(t, swap)

				frag, err := r.LoadFragment("tone", tc.path(repo.ext), baseDir)
				if err != nil {
					t.Fatalf("LoadFragment: %v", err)
				}
				if frag.Content != insideContent {
					t.Fatalf("content = %q, want %q (read through the swapped ancestor)", frag.Content, insideContent)
				}
			})
		}
	}
}

// A fragment that is an absolute link inside the pack is resolved against the
// anchor the lookup opened. When the anchor's ancestry is swapped meanwhile,
// the link now leads outside that anchor, and the lookup is refused rather
// than read through a second, fresh resolution.
func TestFileRepositories_AncestorSwappedUnderAnAbsoluteLink(t *testing.T) {
	for _, repo := range fileRepos {
		t.Run(repo.name, func(t *testing.T) {
			pack, swap := swapFixture(t, repo.ext)
			shared := filepath.Join(pack, "shared", "tone"+repo.ext)
			writeFragment(t, shared, insideContent)
			symlink(t, shared, filepath.Join(pack, "fragments", "alias"+repo.ext))
			r := repo.open(pack)
			common.SetAfterContainmentCheck(t, func() {
				swap()
				// After the swap, <pack> names the look-alike tree: give it a
				// plain alias file holding the secret.
				writeFragment(t, filepath.Join(pack, "fragments", "alias"+repo.ext), outsideContent)
			})

			frag, err := r.LoadFragment("alias", "../fragments/alias"+repo.ext, filepath.Join(pack, "prompts"))
			if frag != nil && frag.Content == outsideContent {
				t.Fatal("read the secret through the swapped ancestor")
			}
			if !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
				t.Fatalf("err = %v, want persistence.ErrFragmentPathOutsideBase", err)
			}
		})
	}
}

// A pack directory that is itself a symlink to elsewhere, with a prompts
// directory reached through a link inside it, works as a plain directory does:
// name-only lookups, relative paths that climb to the pack, and absolute links
// written through the pack's symlinked path all read the pack's files.
func TestFileRepositories_PackDirectoryReachedThroughASymlink(t *testing.T) {
	for _, repo := range fileRepos {
		t.Run(repo.name, func(t *testing.T) {
			real := filepath.Join(t.TempDir(), "real-pack")
			pack := filepath.Join(t.TempDir(), "pack")
			writeFragment(t, filepath.Join(real, "fragments", "tone"+repo.ext), insideContent)
			if err := os.MkdirAll(filepath.Join(real, "prompts"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(real, "versions", "v2"), 0o750); err != nil {
				t.Fatal(err)
			}
			symlink(t, real, pack)
			symlink(t, "v2", filepath.Join(real, "versions", "current"))
			symlink(t, filepath.Join(pack, "fragments", "tone"+repo.ext), filepath.Join(real, "fragments", "alias"+repo.ext))
			r := repo.open(pack)

			lookups := []struct{ name, path, baseDir string }{
				{"tone", "", ""},
				{"alias", "", ""},
				{"x", "../fragments/tone" + repo.ext, filepath.Join(pack, "prompts")},
				{"x", "../../fragments/tone" + repo.ext, filepath.Join(pack, "versions", "current")},
				{"x", "fragments/alias" + repo.ext, pack},
				{"x", "../fragments/alias" + repo.ext, filepath.Join(pack, "prompts")},
			}
			for _, l := range lookups {
				frag, err := r.LoadFragment(l.name, l.path, l.baseDir)
				if err != nil {
					t.Fatalf("LoadFragment(%q, %q, %q): %v", l.name, l.path, l.baseDir, err)
				}
				if frag.Content != insideContent {
					t.Fatalf("LoadFragment(%q, %q, %q) content = %q, want %q",
						l.name, l.path, l.baseDir, frag.Content, insideContent)
				}
			}
		})
	}
}
