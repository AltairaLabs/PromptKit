package session

import "github.com/AltairaLabs/PromptKit/sdk/v2/internal/duplexcfg"

// The SDK's pipelines run a duplex session's tool calls themselves; package
// sdk marks its sessions' configs so through duplexcfg, which keeps the
// setting off the public DuplexSessionConfig.
//
//nolint:gochecknoinits // registration is how the internal package reaches the unexported field.
func init() {
	duplexcfg.Register(func(cfg any) {
		cfg.(*DuplexSessionConfig).pipelineRunsTools = true
	})
}
