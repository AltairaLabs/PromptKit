package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestApplySelfPlayVADConfig_OnlyDisablesForSelfPlayScenarios is a
// regression test for a real bug observed in the duplex-streaming
// example: a scripted-text scenario sharing an arena config with a
// `self_play` block was getting Gemini's automaticActivityDetection
// disabled, defeating the scenario's declared `turn_detection.mode: asm`
// and producing 6-second inter-turn delays.
//
// The fix gates VAD-disable on whether *this scenario* uses persona-
// driven turns, not whether selfplay is configured at the arena level.
func TestApplySelfPlayVADConfig_OnlyDisablesForSelfPlayScenarios(t *testing.T) {
	// Arena-level selfplay block is populated.
	cfgWithSelfPlay := &config.Config{
		SelfPlay: &config.SelfPlayConfig{
			Roles: []config.SelfPlayRoleGroup{
				{ID: "selfplay-user", Provider: "mock-duplex"},
			},
			Personas: []config.PersonaRef{
				{File: "personas/curious.persona.yaml"},
			},
		},
	}

	t.Run("scripted-text scenario with selfplay configured at arena: VAD stays enabled", func(t *testing.T) {
		de := &DuplexConversationExecutor{}
		req := &ConversationRequest{
			Config: cfgWithSelfPlay,
			Scenario: &config.Scenario{
				ID: "scripted",
				Turns: []config.TurnDefinition{
					{Role: "user", Content: "Hello"},
					{Role: "user", Content: "Tell me a fact"},
				},
			},
		}
		cfg := &providers.StreamingInputConfig{Metadata: map[string]interface{}{}}
		de.applySelfPlayVADConfig(cfg, req)
		_, disabled := cfg.Metadata["vad_disabled"]
		assert.False(t, disabled,
			"scripted-text scenario must NOT disable VAD even when arena has selfplay configured")
	})

	t.Run("selfplay scenario: VAD disabled", func(t *testing.T) {
		de := &DuplexConversationExecutor{}
		req := &ConversationRequest{
			Config: cfgWithSelfPlay,
			Scenario: &config.Scenario{
				ID: "selfplay",
				Turns: []config.TurnDefinition{
					{Role: "user", Persona: "curious-customer"},
				},
			},
		}
		cfg := &providers.StreamingInputConfig{Metadata: map[string]interface{}{}}
		de.applySelfPlayVADConfig(cfg, req)
		v, disabled := cfg.Metadata["vad_disabled"]
		assert.True(t, disabled,
			"persona-driven turn must disable VAD")
		assert.Equal(t, true, v)
	})

	t.Run("no selfplay configured at arena: never disables", func(t *testing.T) {
		de := &DuplexConversationExecutor{}
		req := &ConversationRequest{
			Config: &config.Config{}, // no selfplay block
			Scenario: &config.Scenario{
				ID: "any",
				Turns: []config.TurnDefinition{
					{Role: "user", Persona: "curious"},
				},
			},
		}
		cfg := &providers.StreamingInputConfig{Metadata: map[string]interface{}{}}
		de.applySelfPlayVADConfig(cfg, req)
		_, disabled := cfg.Metadata["vad_disabled"]
		assert.False(t, disabled)
	})
}

// TestBuildInteractiveVADConfig_NoDuplexScenario verifies that when the request
// carries no duplex configuration (nil Scenario.Duplex or nil Scenario), the
// function falls back to the AdaptiveVAD-equipped default config. The returned
// config must have a non-nil VAD set — the AdaptiveVAD branch distinguishes
// interactive console runs from the bare DefaultAudioTurnConfig (SimpleVAD).
//
// This is the "bare interactive run" branch (22% → covered): configs loaded
// for the interactive console via `promptarena chat` do not declare a duplex
// block, so this path fires on every interactive voice session.
func TestBuildInteractiveVADConfig_NoDuplexScenario(t *testing.T) {
	de := &DuplexConversationExecutor{}

	t.Run("nil Scenario.Duplex", func(t *testing.T) {
		req := &ConversationRequest{
			Scenario: &config.Scenario{ID: "no-duplex"},
			Config:   &config.Config{},
		}
		cfg := de.buildInteractiveVADConfig(req)
		// AdaptiveVAD is set on the config — not nil.
		assert.NotNil(t, cfg.VAD,
			"expected AdaptiveVAD to be wired for a scenario without duplex config")
	})

	t.Run("nil Scenario", func(t *testing.T) {
		req := &ConversationRequest{Scenario: nil, Config: &config.Config{}}
		cfg := de.buildInteractiveVADConfig(req)
		assert.NotNil(t, cfg.VAD,
			"expected AdaptiveVAD to be wired when Scenario is nil")
	})
}

// TestBuildInteractiveVADConfig_WithDuplexScenario verifies that when the
// request carries a duplex block, buildInteractiveVADConfig delegates to
// buildVADConfig and returns a valid AudioTurnConfig. The observable difference
// from the NoDuplex path: buildVADConfig does NOT set cfg.VAD (leaves it nil so
// AudioTurnStage creates a SimpleVAD internally), whereas the NoDuplex fallback
// always sets cfg.VAD to an AdaptiveVAD. Asserting VAD == nil here confirms the
// delegation happened rather than the AdaptiveVAD branch firing.
func TestBuildInteractiveVADConfig_WithDuplexScenario(t *testing.T) {
	de := &DuplexConversationExecutor{}
	req := &ConversationRequest{
		Scenario: &config.Scenario{
			ID: "with-duplex",
			Duplex: &config.DuplexConfig{
				Timeout: "10s",
				TurnDetection: &config.TurnDetectionConfig{
					Mode: config.TurnDetectionModeVAD,
				},
			},
		},
		Config: &config.Config{},
	}
	cfg := de.buildInteractiveVADConfig(req)
	// buildVADConfig leaves VAD nil (AudioTurnStage builds its own SimpleVAD).
	// If this were the NoDuplex branch, AdaptiveVAD would be set (non-nil).
	assert.Nil(t, cfg.VAD,
		"expected VAD==nil from buildVADConfig delegation (AudioTurnStage creates its own)")
	// SilenceDuration is non-zero because buildVADConfig calls DefaultAudioTurnConfig.
	assert.NotZero(t, cfg.SilenceDuration,
		"expected SilenceDuration set by DefaultAudioTurnConfig via buildVADConfig")
	// Suppress unused import: stage package is used via stage.AudioTurnConfig in
	// the import block to ensure it is loaded even though cfg is a value type.
	var _ stage.AudioTurnConfig
}
