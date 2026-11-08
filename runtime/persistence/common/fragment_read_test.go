package common

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
)

// fragmentReadFixture is a base with prompts/ and fragments/, a file inside
// the fragments and a file outside the base.
func fragmentReadFixture(t *testing.T) (base, inside, outside string) {
	t.Helper()
	base = t.TempDir()
	for _, d := range []string{"prompts", "fragments"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	inside = filepath.Join(base, "fragments", "real.yaml")
	outside = filepath.Join(t.TempDir(), "secret.yaml")
	for p, c := range map[string]string{inside: "inside", outside: "SECRET"} {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return base, inside, outside
}

// A link that is inside when the path is checked and swapped to point
// outside before the read must not be read: the read itself holds the
// containment, not only the check before it.
func TestReadFragmentFile_LinkSwappedAfterTheCheck(t *testing.T) {
	base, inside, outside := fragmentReadFixture(t)
	link := filepath.Join(base, "fragments", "swap.yaml")
	if err := os.Symlink(inside, link); err != nil {
		t.Skipf("os.Symlink: %v", err)
	}
	afterContainmentCheck = func() {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterContainmentCheck = nil })

	data, _, err := ReadFragmentFile(base, "x", "../fragments/swap.yaml", filepath.Join(base, "prompts"), ".yaml")
	if string(data) == "SECRET" {
		t.Fatal("the swapped link was followed out of the base")
	}
	if !errors.Is(err, persistence.ErrFragmentPathOutsideBase) {
		t.Fatalf("err = %v, want ErrFragmentPathOutsideBase", err)
	}
}

// The read keeps the lookup semantics: a missing file is fs.ErrNotExist, a
// link that stays inside is followed, and the default name-only file is read.
func TestReadFragmentFile_Semantics(t *testing.T) {
	base, inside, _ := fragmentReadFixture(t)
	prompts := filepath.Join(base, "prompts")
	if err := os.Symlink(inside, filepath.Join(base, "fragments", "in.yaml")); err != nil {
		t.Skipf("os.Symlink: %v", err)
	}

	if _, _, err := ReadFragmentFile(base, "x", "../fragments/missing.yaml", prompts, ".yaml"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: err = %v, want fs.ErrNotExist", err)
	}
	for _, rel := range []string{"../fragments/in.yaml", "../fragments/real.yaml"} {
		data, path, err := ReadFragmentFile(base, "x", rel, prompts, ".yaml")
		if err != nil || string(data) != "inside" || path == "" {
			t.Fatalf("%s: %q, %q, %v", rel, data, path, err)
		}
	}
	data, _, err := ReadFragmentFile(base, "real", "", "", ".yaml")
	if err != nil || string(data) != "inside" {
		t.Fatalf("default name: %q, %v", data, err)
	}
}

// A link to nothing is not found, wherever it points.
func TestReadFragmentFile_DanglingLinkIsNotFound(t *testing.T) {
	base, _, _ := fragmentReadFixture(t)
	for name, target := range map[string]string{
		"dangle_in.yaml":  filepath.Join(base, "fragments", "gone.yaml"),
		"dangle_out.yaml": filepath.Join(t.TempDir(), "gone.yaml"),
	} {
		if err := os.Symlink(target, filepath.Join(base, "fragments", name)); err != nil {
			t.Skipf("os.Symlink: %v", err)
		}
		_, _, err := ReadFragmentFile(base, "x", "../fragments/"+name, filepath.Join(base, "prompts"), ".yaml")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s: err = %v, want fs.ErrNotExist", name, err)
		}
	}
}
