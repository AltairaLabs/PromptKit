package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/pkg/config"
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
