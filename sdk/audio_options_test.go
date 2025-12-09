package sdk

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

func TestWithSessionVAD(t *testing.T) {
	vad, err := audio.NewSimpleVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("failed to create VAD: %v", err)
	}

	cfg := &audioSessionConfig{}
	opt := WithSessionVAD(vad)
	opt(cfg)

	if cfg.vad == nil {
		t.Error("VAD should be set")
	}
}

func TestWithSessionTurnDetector(t *testing.T) {
	detector := audio.NewSilenceDetector(500 * time.Millisecond)

	cfg := &audioSessionConfig{}
	opt := WithSessionTurnDetector(detector)
	opt(cfg)

	if cfg.turnDetector == nil {
		t.Error("TurnDetector should be set")
	}
}

func TestWithInterruptionStrategy(t *testing.T) {
	tests := []struct {
		name     string
		strategy audio.InterruptionStrategy
	}{
		{"ignore", audio.InterruptionIgnore},
		{"immediate", audio.InterruptionImmediate},
		{"deferred", audio.InterruptionDeferred},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &audioSessionConfig{}
			opt := WithInterruptionStrategy(tt.strategy)
			opt(cfg)

			if cfg.interruptionStrategy != tt.strategy {
				t.Errorf("expected strategy %v, got %v", tt.strategy, cfg.interruptionStrategy)
			}
		})
	}
}

func TestWithAutoCompleteTurn(t *testing.T) {
	cfg := &audioSessionConfig{}
	opt := WithAutoCompleteTurn()
	opt(cfg)

	if !cfg.autoCompleteTurn {
		t.Error("autoCompleteTurn should be true")
	}
}

func TestWithTTSVoice(t *testing.T) {
	cfg := &ttsConfig{}
	opt := WithTTSVoice("nova")
	opt(cfg)

	if cfg.voice != "nova" {
		t.Errorf("expected voice 'nova', got %q", cfg.voice)
	}
}

func TestWithTTSFormat(t *testing.T) {
	cfg := &ttsConfig{}
	opt := WithTTSFormat(tts.FormatMP3)
	opt(cfg)

	if cfg.format.Name != tts.FormatMP3.Name {
		t.Errorf("expected format mp3, got %q", cfg.format.Name)
	}
}

func TestWithTTSSpeed(t *testing.T) {
	cfg := &ttsConfig{}
	opt := WithTTSSpeed(1.5)
	opt(cfg)

	if cfg.speed != 1.5 {
		t.Errorf("expected speed 1.5, got %f", cfg.speed)
	}
}

func TestWithTTSPitch(t *testing.T) {
	cfg := &ttsConfig{}
	opt := WithTTSPitch(-2)
	opt(cfg)

	if cfg.pitch != -2 {
		t.Errorf("expected pitch -2, got %f", cfg.pitch)
	}
}

func TestWithTTSLanguage(t *testing.T) {
	cfg := &ttsConfig{}
	opt := WithTTSLanguage("fr-FR")
	opt(cfg)

	if cfg.language != "fr-FR" {
		t.Errorf("expected language 'fr-FR', got %q", cfg.language)
	}
}

func TestWithTTSModel(t *testing.T) {
	cfg := &ttsConfig{}
	opt := WithTTSModel("tts-1-hd")
	opt(cfg)

	if cfg.model != "tts-1-hd" {
		t.Errorf("expected model 'tts-1-hd', got %q", cfg.model)
	}
}

func TestTTSOptionsCombined(t *testing.T) {
	cfg := &ttsConfig{}
	opts := []TTSOption{
		WithTTSVoice("alloy"),
		WithTTSFormat(tts.FormatOpus),
		WithTTSSpeed(1.25),
		WithTTSPitch(3),
		WithTTSLanguage("en-US"),
		WithTTSModel("tts-1"),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.voice != "alloy" {
		t.Errorf("voice: expected 'alloy', got %q", cfg.voice)
	}
	if cfg.format.Name != tts.FormatOpus.Name {
		t.Errorf("format: expected opus, got %q", cfg.format.Name)
	}
	if cfg.speed != 1.25 {
		t.Errorf("speed: expected 1.25, got %f", cfg.speed)
	}
	if cfg.pitch != 3 {
		t.Errorf("pitch: expected 3, got %f", cfg.pitch)
	}
	if cfg.language != "en-US" {
		t.Errorf("language: expected 'en-US', got %q", cfg.language)
	}
	if cfg.model != "tts-1" {
		t.Errorf("model: expected 'tts-1', got %q", cfg.model)
	}
}
