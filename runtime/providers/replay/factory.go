package replay

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

//nolint:gochecknoinits // Factory registration requires init()
func init() {
	providers.RegisterProviderFactory("replay", createReplayProvider)
}

// createReplayProvider creates a replay provider from a spec.
// Supports two modes:
// - recording: path to a SessionRecording file (for standard Predict/PredictStream)
// - arena_output: path to an arena run output JSON (for duplex streaming replay)
//
//nolint:gocritic // hugeParam: ProviderSpec passed by value to match factory interface
func createReplayProvider(spec providers.ProviderSpec) (providers.Provider, error) {
	cfg := parseConfig(spec.AdditionalConfig)

	// Check for arena output mode (duplex streaming replay)
	if arenaPath, ok := spec.AdditionalConfig["arena_output"].(string); ok && arenaPath != "" {
		return createStreamingProvider(arenaPath, spec.ID, &cfg)
	}

	// Standard recording mode
	recordingPath, ok := spec.AdditionalConfig["recording"].(string)
	if !ok || recordingPath == "" {
		return nil, fmt.Errorf("replay provider requires 'recording' or 'arena_output' path in additional_config")
	}

	// Auto-detect arena output format (JSON files from arena runs)
	if isArenaOutputPath(recordingPath) {
		if p, err := createStreamingProvider(recordingPath, spec.ID, &cfg); err == nil {
			return p, nil
		}
		// Fall through to standard recording if arena format fails
	}

	return createStandardProvider(recordingPath, spec.ID, &cfg)
}

// isArenaOutputPath checks if the path looks like an arena output file.
func isArenaOutputPath(path string) bool {
	return strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".recording.json")
}

// createStreamingProvider creates a streaming provider from an arena output file.
func createStreamingProvider(path, id string, cfg *Config) (*StreamingProvider, error) {
	p, err := NewStreamingProviderFromArenaOutput(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("create streaming replay provider: %w", err)
	}
	if id != "" {
		p.id = id
	}
	return p, nil
}

// createStandardProvider creates a standard replay provider from a recording file.
func createStandardProvider(path, id string, cfg *Config) (*Provider, error) {
	p, err := NewProviderFromFile(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("create replay provider: %w", err)
	}
	if id != "" {
		p.id = id
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
