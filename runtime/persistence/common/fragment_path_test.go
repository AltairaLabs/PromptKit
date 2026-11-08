package common

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
)

func TestFragmentFilePath(t *testing.T) {
	base := t.TempDir()
	prompts := filepath.Join(base, "prompts")
	abs := filepath.Join(t.TempDir(), "elsewhere.yaml")
	cases := []struct {
		name, rel, baseDir, want string
		outside                  bool
	}{
		{name: "default", want: filepath.Join(base, "fragments", "tone.yaml")},
		{name: "sibling dir", rel: "../fragments/tone.yaml", baseDir: prompts, want: filepath.Join(base, "fragments", "tone.yaml")},
		{name: "absolute from config", rel: abs, baseDir: prompts, want: abs},
		{name: "leading climbs are the config's", rel: "../../secret.yaml", baseDir: prompts,
			want: filepath.Join(filepath.Dir(base), "secret.yaml")},
		{name: "climbs back past its start", rel: "x/../../secret.yaml", baseDir: prompts, outside: true},
		{name: "working directory fallback climbs back", rel: "x/../../x.yaml", outside: true},
		{name: "working directory fallback leading climb", rel: "../x.yaml", want: filepath.Join("..", "x.yaml")},
		{name: "working directory fallback", rel: "x.yaml", want: "x.yaml"},
		{name: "prompt dir mapped outside the base", rel: "fragments/tone.yaml",
			baseDir: abs + ".d", want: filepath.Join(abs+".d", "fragments", "tone.yaml")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FragmentFilePath(base, "tone", c.rel, c.baseDir, ".yaml")
			if c.outside {
				if !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
					t.Fatalf("err = %v, want ErrFragmentPathOutsideBase", err)
				}
				if got := errors.Is(err, fs.ErrNotExist); got != (c.baseDir == "") {
					t.Fatalf("fs.ErrNotExist = %v for baseDir %q", got, c.baseDir)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

// A default lookup's name cannot climb out of the fragments directory.
func TestFragmentFilePath_DefaultNameStaysInFragments(t *testing.T) {
	_, err := FragmentFilePath(t.TempDir(), "../../secret", "", "", ".yaml")
	if !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
		t.Fatalf("err = %v, want ErrFragmentPathOutsideBase", err)
	}
}

// Containment holds on the file a path resolves to: a link inside the base
// that leaves it is refused, one that stays in is followed, and a link to
// nothing is left for the read to report as missing.
func TestFragmentFilePath_Symlinks(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	frags := filepath.Join(base, "fragments")
	if err := os.MkdirAll(frags, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frags, "real.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"out.yaml":    outside,
		"in.yaml":     filepath.Join(frags, "real.yaml"),
		"dangle.yaml": filepath.Join(t.TempDir(), "missing.yaml"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(frags, name)); err != nil {
			t.Skipf("os.Symlink: %v", err)
		}
	}
	prompts := filepath.Join(base, "prompts")

	if _, err := FragmentFilePath(base, "x", "../fragments/out.yaml", prompts, ".yaml"); !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
		t.Fatalf("link out: err = %v", err)
	}
	if _, err := FragmentFilePath(base, "out", "", "", ".yaml"); !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
		t.Fatalf("default name through a link out: err = %v", err)
	}
	if got, err := FragmentFilePath(base, "x", "../fragments/in.yaml", prompts, ".yaml"); err != nil || got == "" {
		t.Fatalf("link in: %q, %v", got, err)
	}
	got, err := FragmentFilePath(base, "x", "../fragments/dangle.yaml", prompts, ".yaml")
	if err != nil {
		t.Fatalf("dangling link: %v", err)
	}
	if _, readErr := os.ReadFile(got); !errors.Is(readErr, fs.ErrNotExist) {
		t.Fatalf("reading a dangling link: %v, want fs.ErrNotExist", readErr)
	}
}
