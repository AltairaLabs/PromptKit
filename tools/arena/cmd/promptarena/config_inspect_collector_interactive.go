package main

import (
	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
)

// collectInspectionData collects inspection data and optionally adds cache stats
// based on the inspectStats flag.
func collectInspectionData(cfg *config.Config, configFile string) *inspect.InspectionData {
	data := inspect.CollectInspectionData(cfg, configFile)
	if inspectStats {
		data.CacheStats = inspect.CollectCacheStats(cfg)
	}
	return data
}

// collectConnectivityChecks is a shim for use by runConfigInspect.
func collectConnectivityChecks(data *inspect.InspectionData) []inspect.ValidationCheckData {
	return inspect.CollectConnectivityChecks(data)
}
