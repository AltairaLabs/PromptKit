package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrGitNotFound reports that no usable git executable could be resolved.
// Callers can match it with errors.Is to tell "git is missing or untrustworthy"
// apart from "the git command ran and failed".
var ErrGitNotFound = errors.New("git executable not found")

// gitPath resolves the git executable to an absolute path.
//
// Passing the bare name "git" to exec leaves the lookup to the runtime, which
// re-searches PATH on each call and runs whatever it matches first. Resolving
// here instead means every invocation below names one fixed executable, and the
// two failure modes worth separating are handled explicitly:
//
//   - git is not installed. Left to exec, that surfaces as an opaque failure
//     from Run() after the caller has already committed to an install; here it
//     is a named error before anything happens.
//   - PATH contains a directory relative to the working directory, so the match
//     resolves against wherever the process happens to be running rather than a
//     fixed location. exec.LookPath reports that case as ErrDot, which the
//     error check below treats as fatal.
//
// The IsAbs check behind it is not redundant: GODEBUG=execerrdot=0 switches the
// ErrDot reporting back off, and a relative match still must not be executed
// when it does.
func gitPath() (string, error) {
	found, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGitNotFound, err)
	}
	if !filepath.IsAbs(found) {
		return "", fmt.Errorf("%w: %q resolved through a relative PATH entry", ErrGitNotFound, found)
	}
	return found, nil
}

// defaultGitClone runs "git clone" to clone a repository.
// The URL is constructed from a validated SkillRef, not user input.
func defaultGitClone(url, dest string) error {
	git, err := gitPath()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(
		context.Background(),
		git, "clone", "--depth", "1", url, dest,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// defaultGitCheckout runs "git checkout" in the given directory.
// The ref is a version tag from a validated SkillRef, not user input.
func defaultGitCheckout(dir, ref string) error {
	git, err := gitPath()
	if err != nil {
		return err
	}

	// For shallow clones with a specific tag/version, we need to fetch it first.
	fetchCmd := exec.CommandContext(
		context.Background(),
		git, "fetch", "--depth", "1", "origin", ref,
	)
	fetchCmd.Dir = dir
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return err
	}

	cmd := exec.CommandContext(
		context.Background(),
		git, "checkout", ref,
	)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
