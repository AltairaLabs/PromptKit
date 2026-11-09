package common

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
)

// TestReadFragmentFile_EscapeIsNotFoundOnlyForWorkingDirFallback pins which
// refused reads also satisfy fs.ErrNotExist. A link swapped to point outside
// after the check is always ErrFragmentPathOutsideBase. Only the
// working-directory fallback (a relative path with no baseDir), which
// FragmentResolver tries after a miss, also reports it as not found, so the
// resolver reports the miss. A lookup against a baseDir, or a default path,
// must surface the refusal rather than look like a missing file.
func TestReadFragmentFile_EscapeIsNotFoundOnlyForWorkingDirFallback(t *testing.T) {
	tests := []struct {
		name         string
		relativePath string
		useBaseDir   bool
		wantNotExist bool
	}{
		{"relative path against baseDir", "../fragments/swap.yaml", true, false},
		{"relative path, working-directory fallback", "../fragments/swap.yaml", false, true},
		{"default path", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, inside, outside := fragmentReadFixture(t)
			link := filepath.Join(base, "fragments", "swap.yaml")
			if err := os.Symlink(inside, link); err != nil {
				t.Skipf("os.Symlink: %v", err)
			}
			SetBeforeRootedRead(t, func() {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
			})

			baseDir := filepath.Join(base, "prompts")
			if !tt.useBaseDir {
				t.Chdir(baseDir)
				baseDir = ""
			}
			data, _, err := ReadFragmentFile(base, "swap", tt.relativePath, baseDir, ".yaml")

			if string(data) == "SECRET" {
				t.Fatal("the swapped link was followed out of the base")
			}
			if !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
				t.Fatalf("err = %v, want ErrFragmentPathOutsideBase", err)
			}
			if got := errors.Is(err, fs.ErrNotExist); got != tt.wantNotExist {
				t.Errorf("errors.Is(err, fs.ErrNotExist) = %v, want %v (err: %v)", got, tt.wantNotExist, err)
			}
		})
	}
}

// A link that already points outside when the lookup starts is refused the
// same way as one swapped in: ErrFragmentPathOutsideBase, which only the
// working-directory fallback also reports as not found.
func TestReadFragmentFile_StaticLinkOutMatchesTheSwappedOne(t *testing.T) {
	for _, useBaseDir := range []bool{true, false} {
		t.Run(map[bool]string{true: "against baseDir", false: "working-directory fallback"}[useBaseDir], func(t *testing.T) {
			base, _, outside := fragmentReadFixture(t)
			if err := os.Symlink(outside, filepath.Join(base, "fragments", "out.yaml")); err != nil {
				t.Skipf("os.Symlink: %v", err)
			}
			baseDir := filepath.Join(base, "prompts")
			if !useBaseDir {
				t.Chdir(baseDir)
				baseDir = ""
			}
			data, _, err := ReadFragmentFile(base, "out", "../fragments/out.yaml", baseDir, ".yaml")
			if string(data) == "SECRET" {
				t.Fatal("followed the link out of the base")
			}
			if !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
				t.Fatalf("err = %v, want ErrFragmentPathOutsideBase", err)
			}
			if got := errors.Is(err, fs.ErrNotExist); got == useBaseDir {
				t.Errorf("errors.Is(err, fs.ErrNotExist) = %v for useBaseDir %v (err: %v)", got, useBaseDir, err)
			}
		})
	}
}
