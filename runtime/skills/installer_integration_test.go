package skills

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGitScript records the arguments it was called with, one invocation per
// line, in the file named by FAKE_GIT_LOG. That lets the tests below assert
// which git commands were issued without a real git being involved. Setting
// FAKE_GIT_FAIL_ON to a subcommand makes that one subcommand exit non-zero.
const fakeGitScript = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GIT_LOG"
if [ -n "$FAKE_GIT_FAIL_ON" ] && [ "$1" = "$FAKE_GIT_FAIL_ON" ]; then
  exit 1
fi
exit 0
`

// writeFakeGit puts an executable named "git" inside dir so PATH lookups match
// it. It returns the path of the log the fake appends its arguments to.
func writeFakeGit(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git relies on a /bin/sh shebang")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(fakeGitScript), 0o500); err != nil {
		t.Fatalf("writing fake git: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "git-invocations.log")
	t.Setenv("FAKE_GIT_LOG", logPath)
	return logPath
}

// gitInvocations returns one entry per call the fake git recorded.
func gitInvocations(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(logPath))
	if err != nil {
		t.Fatalf("reading %s: %v", logPath, err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestGitPathReturnsAbsolutePath(t *testing.T) {
	got, err := gitPath()
	if err != nil {
		t.Skipf("no git on PATH in this environment: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("gitPath() = %q, want an absolute path", got)
	}
}

func TestGitPathReportsErrGitNotFoundWhenMissing(t *testing.T) {
	// An empty directory as the entire PATH: nothing to match.
	t.Setenv("PATH", t.TempDir())

	_, err := gitPath()
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("gitPath() error = %v, want it to wrap ErrGitNotFound", err)
	}
}

// A PATH entry relative to the working directory resolves against wherever the
// process happens to be running, which is the case this guard exists for. It
// must be refused even though a matching executable is genuinely there.
func TestGitPathRejectsRelativePATHEntry(t *testing.T) {
	root := t.TempDir()
	writeFakeGit(t, filepath.Join(root, "fakebin"))

	t.Chdir(root)
	t.Setenv("PATH", "fakebin")

	got, err := gitPath()
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("gitPath() = %q, error = %v; want it to wrap ErrGitNotFound", got, err)
	}
	if got != "" {
		t.Errorf("gitPath() = %q, want no path returned alongside the error", got)
	}
}

// The absolute-path guard must hold even where the runtime's own ErrDot
// reporting is switched off, which is what GODEBUG=execerrdot=0 does. Without
// the IsAbs check this case would resolve and run the relative match.
func TestGitPathRejectsRelativePATHEntryWithoutErrDot(t *testing.T) {
	root := t.TempDir()
	writeFakeGit(t, filepath.Join(root, "fakebin"))

	t.Chdir(root)
	t.Setenv("PATH", "fakebin")
	t.Setenv("GODEBUG", "execerrdot=0")

	if got, err := gitPath(); !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("gitPath() = %q, error = %v; want it to wrap ErrGitNotFound", got, err)
	}
}

func TestDefaultGitCloneRunsResolvedGit(t *testing.T) {
	bin := t.TempDir()
	logPath := writeFakeGit(t, bin)
	t.Setenv("PATH", bin)

	dest := filepath.Join(t.TempDir(), "dest")
	if err := defaultGitClone("https://example.invalid/org/skill.git", dest); err != nil {
		t.Fatalf("defaultGitClone() = %v, want nil", err)
	}

	got := gitInvocations(t, logPath)
	want := "clone --depth 1 https://example.invalid/org/skill.git " + dest
	if len(got) != 1 || got[0] != want {
		t.Errorf("git invocations = %q, want exactly [%q]", got, want)
	}
}

func TestDefaultGitCloneReportsErrGitNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := defaultGitClone("https://example.invalid/org/skill.git", filepath.Join(t.TempDir(), "dest"))
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("defaultGitClone() error = %v, want it to wrap ErrGitNotFound", err)
	}
	if !strings.Contains(err.Error(), "git executable not found") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A shallow clone has to fetch the ref before it can check it out, so both
// commands must be issued, in that order.
func TestDefaultGitCheckoutFetchesThenChecksOut(t *testing.T) {
	bin := t.TempDir()
	logPath := writeFakeGit(t, bin)
	t.Setenv("PATH", bin)

	if err := defaultGitCheckout(t.TempDir(), "v1.0.0"); err != nil {
		t.Fatalf("defaultGitCheckout() = %v, want nil", err)
	}

	got := gitInvocations(t, logPath)
	want := []string{"fetch --depth 1 origin v1.0.0", "checkout v1.0.0"}
	if len(got) != len(want) {
		t.Fatalf("git invocations = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("invocation %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// If the fetch fails there is nothing to check out, so the checkout must not
// be attempted and the fetch's error must reach the caller.
func TestDefaultGitCheckoutStopsWhenFetchFails(t *testing.T) {
	bin := t.TempDir()
	logPath := writeFakeGit(t, bin)
	t.Setenv("PATH", bin)
	t.Setenv("FAKE_GIT_FAIL_ON", "fetch")

	if err := defaultGitCheckout(t.TempDir(), "v1.0.0"); err == nil {
		t.Fatal("defaultGitCheckout() = nil, want the fetch failure")
	}

	if got := gitInvocations(t, logPath); len(got) != 1 {
		t.Errorf("git invocations = %q, want the fetch alone with no checkout after it", got)
	}
}

func TestDefaultGitCheckoutReportsErrGitNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := defaultGitCheckout(t.TempDir(), "v1.0.0")
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("defaultGitCheckout() error = %v, want it to wrap ErrGitNotFound", err)
	}
}
