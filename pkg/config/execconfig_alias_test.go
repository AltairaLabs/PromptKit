package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/execconfig"
)

// The exec/sandbox binding types moved to runtime/hooks/execconfig to break the
// runtime <-> pkg module cycle (issue #1713). They must stay *aliases*, not
// defined types: runtime/hooks takes execconfig.ExecHook while the SDK reads
// config.ExecHook out of a parsed RuntimeConfig, so a defined type would make
// `hooks.BuildExecHooks(spec.Hooks, ...)` stop compiling for every consumer.
func TestExecTypesAreAliasesOfExecconfig(t *testing.T) {
	// Direct assignment in both directions compiles only for an alias. A
	// defined type (`type ExecHook execconfig.ExecHook`) fails to build here.
	var hook execconfig.ExecHook = ExecHook{Hook: "provider"}
	var binding ExecBinding = execconfig.ExecBinding{Command: "./x"}
	var sandbox execconfig.SandboxConfig = SandboxConfig{Mode: "direct"}

	assert.Equal(t, "provider", hook.Hook)
	assert.Equal(t, "./x", binding.Command)
	assert.Equal(t, "direct", sandbox.Mode)

	// Reflect identity is what the schema generator sees: promptarena keys its
	// $defs off the bare type name, and a split type graph would emit two
	// definitions where there is meant to be one.
	assert.Equal(t, reflect.TypeOf(execconfig.ExecHook{}), reflect.TypeOf(ExecHook{}))
	assert.Equal(t, reflect.TypeOf(execconfig.ExecBinding{}), reflect.TypeOf(ExecBinding{}))
	assert.Equal(t, reflect.TypeOf(execconfig.SandboxConfig{}), reflect.TypeOf(SandboxConfig{}))
}

// Both moved structs rely on `yaml:",inline"` — ExecHook embeds ExecBinding
// inline, and SandboxConfig sweeps unknown keys into an inline map. Losing
// either tag in the move would still compile and still generate the same
// schema, but would silently parse real configs into empty fields.
func TestMovedTypesKeepInlineYAMLShape(t *testing.T) {
	const src = `
sandboxes:
  docker:
    mode: docker_run
    image: alpine:3.20
    network: none
hooks:
  audit:
    command: ./hooks/audit
    hook: session
    phases: [before_call]
    sandbox: docker
    timeout_ms: 500
`

	var spec RuntimeConfigSpec
	require.NoError(t, yaml.Unmarshal([]byte(src), &spec))

	sb := spec.Sandboxes["docker"]
	require.NotNil(t, sb)
	assert.Equal(t, "docker_run", sb.Mode)
	// Proves SandboxConfig.Config kept `yaml:",inline"`: mode is a named field,
	// image/network are not and must land in the catch-all map.
	assert.Equal(t, "alpine:3.20", sb.Config["image"])
	assert.Equal(t, "none", sb.Config["network"])
	assert.NotContains(t, sb.Config, "mode")

	h := spec.Hooks["audit"]
	require.NotNil(t, h)
	assert.Equal(t, "session", h.Hook)
	assert.Equal(t, []string{"before_call"}, h.Phases)
	// Promoted from the embedded ExecBinding — empty here if the embed lost
	// its inline tag and started expecting a nested `execBinding:` key.
	assert.Equal(t, "./hooks/audit", h.Command)
	assert.Equal(t, "docker", h.Sandbox)
	assert.Equal(t, 500, h.TimeoutMs)
}
