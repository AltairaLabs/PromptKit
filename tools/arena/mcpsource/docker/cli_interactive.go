package docker

// This file holds the realCLI methods that exec the local `docker` binary.
// It's named *_interactive.go so the pre-commit coverage check excludes it;
// these methods require a live docker daemon to exercise and are covered by
// the build-tagged integration test (integration_test.go).

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Run calls `docker run` with the spec's args and returns the container ID.
func (r *realCLI) Run(ctx context.Context, spec RunSpec) (string, error) {
	return execDockerOutput(ctx, buildRunArgs(spec))
}

// Stop calls `docker stop --time=3 <id>`. Short grace period — the sandbox
// has no stateful shutdown to preserve.
func (r *realCLI) Stop(ctx context.Context, id string) error {
	// #nosec G204 -- docker binary is fixed; id is a container ID we just produced.
	return exec.CommandContext(ctx, "docker", "stop", "--time=3", id).Run()
}

// Exec runs `docker exec <id> <argv...>` and returns stdout.
func (r *realCLI) Exec(ctx context.Context, id string, argv []string) (string, error) {
	full := append([]string{"exec", id}, argv...)
	return execDockerOutput(ctx, full)
}

// execDockerOutput runs the docker binary with the supplied argv and
// returns its trimmed stdout.
func execDockerOutput(ctx context.Context, argv []string) (string, error) {
	// #nosec G204 -- docker binary is fixed; argv is built from RunSpec with typed fields.
	out, err := exec.CommandContext(ctx, "docker", argv...).Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w", strings.Join(argv, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
