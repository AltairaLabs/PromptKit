package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRunArgs_Basic(t *testing.T) {
	args := buildRunArgs(RunSpec{
		Image:    "ghcr.io/altairalabs/codegen-sandbox:latest",
		PortHost: 38081,
		PortCtr:  8080,
	})
	require.NotEmpty(t, args)
	assert.Equal(t, "run", args[0])
	assert.Contains(t, args, "-d")
	assert.Contains(t, args, "--rm")
	assert.Contains(t, args, "-p")
	assert.Contains(t, args, "38081:8080")
	assert.Equal(t, "ghcr.io/altairalabs/codegen-sandbox:latest", args[len(args)-1])
}

func TestBuildRunArgs_EnvAndMounts(t *testing.T) {
	args := buildRunArgs(RunSpec{
		Image:    "x",
		PortHost: 9000,
		PortCtr:  8080,
		Env:      map[string]string{"FOO": "bar"},
		Mounts: []Mount{
			{Source: "/a", Target: "/b", ReadOnly: true},
			{Source: "/c", Target: "/d", ReadOnly: false},
		},
	})
	assert.Contains(t, args, "-e")
	assert.Contains(t, args, "FOO=bar")
	assert.Contains(t, args, "-v")
	assert.Contains(t, args, "/a:/b:ro")
	assert.Contains(t, args, "/c:/d")
}

func TestBuildRunArgs_NoPortMapping(t *testing.T) {
	args := buildRunArgs(RunSpec{Image: "x"})
	for _, a := range args {
		assert.NotEqual(t, "-p", a, "port flag should be absent when port is 0")
	}
}
