package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// TestBuildInteractiveVADConfig_OverrideWins verifies the test-only vadOverride
// takes precedence over the AdaptiveVAD default (used by the deterministic
// multi-turn integration test to inject a scripted VAD).
func TestBuildInteractiveVADConfig_OverrideWins(t *testing.T) {
	de := &DuplexConversationExecutor{}
	override := &scriptedVAD{}
	req := &ConversationRequest{
		Scenario: &config.Scenario{
			Duplex: &config.DuplexConfig{
				TurnDetection: &config.TurnDetectionConfig{Mode: config.TurnDetectionModeVAD},
			},
		},
		vadOverride: override,
	}

	cfg := de.buildInteractiveVADConfig(req)
	if cfg.VAD != override {
		t.Fatalf("vadOverride must win; got %T", cfg.VAD)
	}
}
