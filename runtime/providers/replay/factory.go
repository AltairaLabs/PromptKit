package replay

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

//nolint:gochecknoinits // Factory registration requires init()
func init() {
	providers.RegisterProviderFactory("replay", createReplayProvider)
}

// createReplayProvider creates a replay provider from a spec.
//
//nolint:gocritic // hugeParam: ProviderSpec passed by value to match factory interface
func createReplayProvider(spec providers.ProviderSpec) (providers.Provider, error) {
	recordingPath, ok := spec.AdditionalConfig["recording"].(string)
	if !ok || recordingPath == "" {
		return nil, fmt.Errorf("replay provider requires 'recording' path in additional_config")
	}

	cfg := parseConfig(spec.AdditionalConfig)

	p, err := NewProviderFromFile(recordingPath, &cfg)
	if err != nil {
		return nil, fmt.Errorf("create replay provider: %w", err)
	}

	if spec.ID != "" {
		p.id = spec.ID
	}

	return p, nil
}

func parseConfig(additionalConfig map[string]interface{}) Config {
	cfg := DefaultConfig()

	if timing, ok := additionalConfig["timing"].(string); ok {
		cfg.Timing = parseTimingMode(timing)
	}

	if speed, ok := additionalConfig["speed"].(float64); ok {
		cfg.Speed = speed
	}

	if match, ok := additionalConfig["match"].(string); ok {
		cfg.MatchMode = parseMatchMode(match)
	}

	return cfg
}

func parseTimingMode(timing string) TimingMode {
	switch timing {
	case "realtime":
		return TimingRealTime
	case "accelerated":
		return TimingAccelerated
	default:
		return TimingInstant
	}
}

func parseMatchMode(match string) MatchMode {
	switch match {
	case "content":
		return MatchByContent
	default:
		return MatchByTurn
	}
}
