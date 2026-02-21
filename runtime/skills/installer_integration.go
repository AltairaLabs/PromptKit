package skills

import (
	"context"
	"os"
	"os/exec"
)

// defaultGitClone runs "git clone" to clone a repository.
// The URL is constructed from a validated SkillRef, not user input.
func defaultGitClone(url, dest string) error {
	cmd := exec.CommandContext(
		context.Background(),
		"git", "clone", "--depth", "1", url, dest,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// defaultGitCheckout runs "git checkout" in the given directory.
// The ref is a version tag from a validated SkillRef, not user input.
func defaultGitCheckout(dir, ref string) error {
	// For shallow clones with a specific tag/version, we need to fetch it first.
	fetchCmd := exec.CommandContext(
		context.Background(),
		"git", "fetch", "--depth", "1", "origin", ref,
	)
	fetchCmd.Dir = dir
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return err
	}

	cmd := exec.CommandContext(
		context.Background(),
		"git", "checkout", ref,
	)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
