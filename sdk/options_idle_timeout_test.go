package sdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// TestWithIdleTimeout_ReachesPipelineConfig guards the wiring, not the option:
// an option that never arrives leaves every turn on the 30s default while the
// caller believes it raised the bound.
func TestWithIdleTimeout_ReachesPipelineConfig(t *testing.T) {
	conv := &Conversation{
		config:       configFromOptions(t, WithIdleTimeout(90*time.Second)),
		toolRegistry: tools.NewRegistry(),
		prompt:       &pack.Prompt{},
	}

	cfg := conv.buildPipelineConfig(nil, "conv-1", nil, nil)

	require.NotNil(t, cfg)
	require.NotNil(t, cfg.IdleTimeout, "WithIdleTimeout did not reach the pipeline config")
	assert.Equal(t, 90*time.Second, *cfg.IdleTimeout)
}

// The unset case — no option, runtime default of 30s applies — is asserted
// where the value actually resolves, in
// internal/pipeline.TestPipelineConfigFor_IdleTimeout. Asserting a nil pointer
// here would pass whether or not the default survived.
