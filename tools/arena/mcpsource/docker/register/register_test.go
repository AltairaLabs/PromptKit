package register

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerSourceRegistered(t *testing.T) {
	src, ok := mcpsource.LookupMCPSource("docker")
	require.True(t, ok, "docker source should be registered via init()")
	assert.NotNil(t, src)
}
