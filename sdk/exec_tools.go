package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// registerExecExecutor registers the exec executor and applies exec tool configs
// to matching tool descriptors in the registry. Called during pipeline construction.
func (c *Conversation) registerExecExecutor() {
	if len(c.config.execToolConfigs) == 0 {
		return
	}

	// Register the exec executor
	c.toolRegistry.RegisterExecutor(&tools.ExecExecutor{})

	// Apply exec configs to matching tool descriptors
	for name, execCfg := range c.config.execToolConfigs {
		td := c.toolRegistry.Get(name)
		if td == nil {
			continue
		}
		td.Mode = "exec"
		td.ExecConfig = execCfg
	}
}
