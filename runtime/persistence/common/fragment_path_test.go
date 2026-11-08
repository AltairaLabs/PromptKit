package common

import (
	"errors"
	"io/fs"
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
		{name: "climbs out of the base", rel: "../../secret.yaml", baseDir: prompts, outside: true},
		{name: "working directory fallback stays in it", rel: "../x.yaml", outside: true},
		{name: "working directory fallback", rel: "x.yaml", want: "x.yaml"},
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
