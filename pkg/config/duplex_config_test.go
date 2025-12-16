package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDuplexConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *DuplexConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "empty config is valid",
			config:  &DuplexConfig{},
			wantErr: false,
		},
		{
			name: "valid timeout",
			config: &DuplexConfig{
				Timeout: "10m",
			},
			wantErr: false,
		},
		{
			name: "valid timeout with seconds",
			config: &DuplexConfig{
				Timeout: "5m30s",
			},
			wantErr: false,
		},
		{
			name: "invalid timeout format",
			config: &DuplexConfig{
				Timeout: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid duplex timeout format",
		},
		{
			name: "valid VAD mode",
			config: &DuplexConfig{
				TurnDetection: &TurnDetectionConfig{
					Mode: TurnDetectionModeVAD,
				},
			},
			wantErr: false,
		},
		{
			name: "valid ASM mode",
			config: &DuplexConfig{
				TurnDetection: &TurnDetectionConfig{
					Mode: TurnDetectionModeASM,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid turn detection mode",
			config: &DuplexConfig{
				TurnDetection: &TurnDetectionConfig{
					Mode: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid turn detection mode",
		},
		{
			name: "valid VAD config",
			config: &DuplexConfig{
				Timeout: "10m",
				TurnDetection: &TurnDetectionConfig{
					Mode: TurnDetectionModeVAD,
					VAD: &VADConfig{
						SilenceThresholdMs: 600,
						MinSpeechMs:        1000,
						MaxTurnDurationS:   60,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "negative silence threshold",
			config: &DuplexConfig{
				TurnDetection: &TurnDetectionConfig{
					Mode: TurnDetectionModeVAD,
					VAD: &VADConfig{
						SilenceThresholdMs: -1,
					},
				},
			},
			wantErr: true,
			errMsg:  "silence_threshold_ms must be non-negative",
		},
		{
			name: "negative min speech",
			config: &DuplexConfig{
				TurnDetection: &TurnDetectionConfig{
					Mode: TurnDetectionModeVAD,
					VAD: &VADConfig{
						MinSpeechMs: -1,
					},
				},
			},
			wantErr: true,
			errMsg:  "min_speech_ms must be non-negative",
		},
		{
			name: "negative max turn duration",
			config: &DuplexConfig{
				TurnDetection: &TurnDetectionConfig{
					Mode: TurnDetectionModeVAD,
					VAD: &VADConfig{
						MaxTurnDurationS: -1,
					},
				},
			},
			wantErr: true,
			errMsg:  "max_turn_duration_s must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestDuplexConfig_GetTimeoutDuration(t *testing.T) {
	defaultTimeout := 5 * time.Minute

	tests := []struct {
		name     string
		config   *DuplexConfig
		expected time.Duration
	}{
		{
			name:     "nil config returns default",
			config:   nil,
			expected: defaultTimeout,
		},
		{
			name:     "empty timeout returns default",
			config:   &DuplexConfig{},
			expected: defaultTimeout,
		},
		{
			name: "valid timeout",
			config: &DuplexConfig{
				Timeout: "10m",
			},
			expected: 10 * time.Minute,
		},
		{
			name: "invalid timeout returns default",
			config: &DuplexConfig{
				Timeout: "invalid",
			},
			expected: defaultTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetTimeoutDuration(defaultTimeout)
			if got != tt.expected {
				t.Errorf("GetTimeoutDuration() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTTSConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *TTSConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name: "valid config",
			config: &TTSConfig{
				Provider: "openai",
				Voice:    "nova",
			},
			wantErr: false,
		},
		{
			name: "missing provider",
			config: &TTSConfig{
				Voice: "nova",
			},
			wantErr: true,
			errMsg:  "tts provider is required",
		},
		{
			name: "missing voice",
			config: &TTSConfig{
				Provider: "openai",
			},
			wantErr: true,
			errMsg:  "tts voice is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestScenario_DuplexParsing(t *testing.T) {
	yamlContent := `
id: voice-interview-test
task_type: interviewer
description: "Voice interview with duplex streaming"
duplex:
  timeout: "10m"
  turn_detection:
    mode: vad
    vad:
      silence_threshold_ms: 600
      min_speech_ms: 1000
      max_turn_duration_s: 60
turns:
  - role: user
    parts:
      - type: audio
        media:
          file_path: "audio/response.wav"
          mime_type: "audio/wav"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if scenario.Duplex == nil {
		t.Fatal("Expected duplex config to be parsed")
	}

	if scenario.Duplex.Timeout != "10m" {
		t.Errorf("Expected timeout '10m', got %q", scenario.Duplex.Timeout)
	}

	if scenario.Duplex.TurnDetection == nil {
		t.Fatal("Expected turn_detection to be parsed")
	}

	if scenario.Duplex.TurnDetection.Mode != "vad" {
		t.Errorf("Expected mode 'vad', got %q", scenario.Duplex.TurnDetection.Mode)
	}

	if scenario.Duplex.TurnDetection.VAD == nil {
		t.Fatal("Expected VAD config to be parsed")
	}

	vad := scenario.Duplex.TurnDetection.VAD
	if vad.SilenceThresholdMs != 600 {
		t.Errorf("Expected silence_threshold_ms 600, got %d", vad.SilenceThresholdMs)
	}
	if vad.MinSpeechMs != 1000 {
		t.Errorf("Expected min_speech_ms 1000, got %d", vad.MinSpeechMs)
	}
	if vad.MaxTurnDurationS != 60 {
		t.Errorf("Expected max_turn_duration_s 60, got %d", vad.MaxTurnDurationS)
	}

	// Validate the parsed config
	if err := scenario.Duplex.Validate(); err != nil {
		t.Errorf("Parsed duplex config validation failed: %v", err)
	}
}

func TestTurnDefinition_TTSParsing(t *testing.T) {
	yamlContent := `
id: selfplay-voice-test
task_type: interviewer
description: "Self-play voice interview"
duplex:
  timeout: "15m"
  turn_detection:
    mode: vad
turns:
  - role: gemini-user
    persona: senior-engineer
    turns: 5
    tts:
      provider: openai
      voice: nova
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if len(scenario.Turns) != 1 {
		t.Fatalf("Expected 1 turn, got %d", len(scenario.Turns))
	}

	turn := scenario.Turns[0]
	if turn.TTS == nil {
		t.Fatal("Expected TTS config to be parsed")
	}

	if turn.TTS.Provider != "openai" {
		t.Errorf("Expected provider 'openai', got %q", turn.TTS.Provider)
	}
	if turn.TTS.Voice != "nova" {
		t.Errorf("Expected voice 'nova', got %q", turn.TTS.Voice)
	}

	// Validate the TTS config
	if err := turn.TTS.Validate(); err != nil {
		t.Errorf("Parsed TTS config validation failed: %v", err)
	}
}

func TestScenario_BackwardCompatibility(t *testing.T) {
	// Ensure scenarios without duplex config still parse correctly
	yamlContent := `
id: standard-scenario
task_type: support
description: "Standard scenario without duplex"
turns:
  - role: user
    content: "Hello"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if scenario.Duplex != nil {
		t.Error("Expected duplex to be nil for non-duplex scenario")
	}

	if len(scenario.Turns) != 1 {
		t.Errorf("Expected 1 turn, got %d", len(scenario.Turns))
	}

	if scenario.Turns[0].TTS != nil {
		t.Error("Expected TTS to be nil for standard turn")
	}
}

// containsStr checks if s contains substr
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchStr(s, substr)))
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
