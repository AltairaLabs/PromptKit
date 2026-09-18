package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

func TestPipelineConfigFor_IdleTimeout(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		pc := pipelineConfigFor(&Config{})
		assert.Equal(t, stage.DefaultIdleTimeoutSeconds*time.Second, pc.IdleTimeout)
	})

	t.Run("honors an override", func(t *testing.T) {
		idle := 90 * time.Second
		pc := pipelineConfigFor(&Config{IdleTimeout: &idle})
		assert.Equal(t, idle, pc.IdleTimeout)
	})

	t.Run("zero disables the idle timer", func(t *testing.T) {
		idle := time.Duration(0)
		pc := pipelineConfigFor(&Config{IdleTimeout: &idle})
		assert.Equal(t, time.Duration(0), pc.IdleTimeout)
	})

	t.Run("applies to duplex, which only force-disables ExecutionTimeout", func(t *testing.T) {
		idle := 45 * time.Second
		pc := pipelineConfigFor(&Config{
			StreamInputProvider: mock.NewStreamingProvider("test", "test-model", false),
			IdleTimeout:         &idle,
		})
		assert.Equal(t, idle, pc.IdleTimeout)
		assert.Equal(t, time.Duration(0), pc.ExecutionTimeout)
	})
}
