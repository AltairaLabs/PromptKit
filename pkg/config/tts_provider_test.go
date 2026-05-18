package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProvider_TTSFieldsRoundTrip(t *testing.T) {
	src := `id: cartesia-confident-man
type: cartesia
role: tts
voice: bf991597-6c13-47e4-8411-91ec2de5c466
sample_rate: 24000
`
	var p Provider
	if err := yaml.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if p.GetRole() != RoleTTS {
		t.Fatalf("capability: got %q, want tts", p.GetRole())
	}
	if p.Voice != "bf991597-6c13-47e4-8411-91ec2de5c466" {
		t.Fatalf("voice: got %q", p.Voice)
	}
	if p.SampleRate != 24000 {
		t.Fatalf("sample_rate: got %d, want 24000", p.SampleRate)
	}
}

func TestProvider_MockTTSAudioFiles(t *testing.T) {
	src := `id: mock-tts
type: mock
role: tts
audio_files:
  - audio/a.pcm
  - audio/b.pcm
`
	var p Provider
	if err := yaml.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(p.AudioFiles) != 2 {
		t.Fatalf("audio_files: got %d entries, want 2", len(p.AudioFiles))
	}
}

func TestScenario_VoiceFieldRoundTrip(t *testing.T) {
	src := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: t
spec:
  id: t
  task_type: voice-assistant
  voice: alloy
  turns:
    - role: user
      content: hi
`
	var sc struct {
		Spec Scenario `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(src), &sc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sc.Spec.Voice != "alloy" {
		t.Fatalf("voice: got %q, want alloy", sc.Spec.Voice)
	}
}

func TestPersona_VoiceField(t *testing.T) {
	src := `id: aggressive-entitled
description: hostile caller
voice: confident-man
`
	var pp UserPersonaPack
	if err := yaml.Unmarshal([]byte(src), &pp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if pp.Voice != "confident-man" {
		t.Fatalf("voice: got %q, want confident-man", pp.Voice)
	}
}
