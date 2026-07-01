package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

// TestBuildInteractiveVADConfig_OverrideWins verifies the test-only vadOverride
// takes precedence over the AdaptiveVAD default (used by the deterministic
// multi-turn integration test to inject a scripted VAD).
func TestBuildInteractiveVADConfig_OverrideWins(t *testing.T) {
	de := &DuplexConversationExecutor{}
	override := &scriptedVAD{}
	req := &ConversationRequest{
		Scenario: &arenaconfig.Scenario{
			Duplex: &arenaconfig.DuplexConfig{
				TurnDetection: &arenaconfig.TurnDetectionConfig{Mode: arenaconfig.TurnDetectionModeVAD},
			},
		},
		vadOverride: override,
	}

	cfg := de.buildInteractiveVADConfig(req)
	if cfg.VAD != override {
		t.Fatalf("vadOverride must win; got %T", cfg.VAD)
	}
}

// TestBuildInteractiveTTSConfig_ResolvesVendorVoice pins the fix for "no audio
// comes back": VoiceOutputVoice is a voices: binding id (e.g. "agent-voice"),
// which must resolve to the bound provider's vendor voice ("alloy"). Passing the
// binding id straight through made OpenAI reject every synthesis.
func TestBuildInteractiveTTSConfig_ResolvesVendorVoice(t *testing.T) {
	de := &DuplexConversationExecutor{}
	cfg := &arenaconfig.Config{
		LoadedTTSProviders: map[string]*config.Provider{
			"openai-tts": {ID: "openai-tts", Role: config.RoleTTS, Voice: "alloy"},
		},
		Voices: []config.VoiceBinding{{ID: "agent-voice", Provider: "openai-tts"}},
	}
	req := &ConversationRequest{Config: cfg, VoiceOutputVoice: "agent-voice"}

	got := de.buildInteractiveTTSConfig(req)
	if got.Voice != "alloy" {
		t.Fatalf("expected resolved vendor voice %q, got %q (binding id leaked through?)", "alloy", got.Voice)
	}
}
