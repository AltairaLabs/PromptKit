package duplexcfg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// RunToolsInPipeline hands the config it is given to the registered setter.
func TestRunToolsInPipeline_CallsTheRegisteredSetter(t *testing.T) {
	saved := runToolsInPipeline
	t.Cleanup(func() { runToolsInPipeline = saved })
	var got []any
	Register(func(cfg any) { got = append(got, cfg) })

	cfg := &struct{ n int }{n: 7}
	RunToolsInPipeline(cfg)
	assert.Equal(t, []any{cfg}, got)
}
