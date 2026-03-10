package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

const serverRuntime = "server"

// registerExecExecutor registers the exec and server executors, then applies
// exec tool configs to matching tool descriptors in the registry.
// Called during pipeline construction.
func (c *Conversation) registerExecExecutor() {
	if len(c.config.execToolConfigs) == 0 {
		return
	}

	var hasExec, hasServer bool
	for _, cfg := range c.config.execToolConfigs {
		if cfg.Runtime == serverRuntime {
			hasServer = true
		} else {
			hasExec = true
		}
	}

	if hasExec {
		c.toolRegistry.RegisterExecutor(&tools.ExecExecutor{})
	}
	if hasServer {
		se := &tools.ServerExecutor{}
		c.toolRegistry.RegisterExecutor(se)
		c.serverExecutor = se
	}

	// Apply exec configs to matching tool descriptors
	for name, execCfg := range c.config.execToolConfigs {
		td := c.toolRegistry.Get(name)
		if td == nil {
			continue
		}
		if execCfg.Runtime == serverRuntime {
			td.Mode = "server"
		} else {
			td.Mode = "exec"
		}
		td.ExecConfig = execCfg
	}
}
