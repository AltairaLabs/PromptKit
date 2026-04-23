// Package register wires the Docker MCPSource into the global registry
// via a blank import. The default promptarena binary imports this
// package for batteries-included behavior; custom binaries can omit it
// and register their own source set.
package register

import (
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource/docker"
)

//nolint:gochecknoinits // registration-by-blank-import requires init()
func init() {
	mcpsource.RegisterMCPSource("docker", docker.New())
}
