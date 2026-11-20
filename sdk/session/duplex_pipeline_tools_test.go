package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/duplexcfg"
)

// The SDK marks a config through duplexcfg as one whose pipeline runs the
// session's tool calls; a config it does not mark runs them in the session.
func TestRunToolsInPipeline_MarksTheConfig(t *testing.T) {
	marked, unmarked := &DuplexSessionConfig{}, &DuplexSessionConfig{}
	duplexcfg.RunToolsInPipeline(marked)
	assert.True(t, marked.pipelineRunsTools)
	assert.False(t, unmarked.pipelineRunsTools)
}
