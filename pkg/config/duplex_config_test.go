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

func TestDuplexConfig_GetResilience(t *testing.T) {
	tests := []struct {
		name   string
		config *DuplexConfig
		isNil  bool
	}{
		{
			name:   "nil config returns nil",
			config: nil,
			isNil:  true,
		},
		{
			name:   "config without resilience returns nil",
			config: &DuplexConfig{},
			isNil:  true,
		},
		{
			name: "config with resilience returns resilience",
			config: &DuplexConfig{
				Resilience: &DuplexResilienceConfig{
					MaxRetries: 3,
				},
			},
			isNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetResilience()
			if tt.isNil && got != nil {
				t.Errorf("GetResilience() expected nil, got %v", got)
			}
			if !tt.isNil && got == nil {
				t.Error("GetResilience() expected non-nil, got nil")
			}
		})
	}
}

func TestDuplexResilienceConfig_GetMaxRetries(t *testing.T) {
	defaultVal := 5
	tests := []struct {
		name     string
		config   *DuplexResilienceConfig
		expected int
	}{
		{
			name:     "nil config returns default",
			config:   nil,
			expected: defaultVal,
		},
		{
			name:     "zero value returns default",
			config:   &DuplexResilienceConfig{MaxRetries: 0},
			expected: defaultVal,
		},
		{
			name:     "negative value returns default",
			config:   &DuplexResilienceConfig{MaxRetries: -1},
			expected: defaultVal,
		},
		{
			name:     "positive value returns configured value",
			config:   &DuplexResilienceConfig{MaxRetries: 3},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetMaxRetries(defaultVal)
			if got != tt.expected {
				t.Errorf("GetMaxRetries() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDuplexResilienceConfig_GetRetryDelayMs(t *testing.T) {
	defaultVal := 1000
	tests := []struct {
		name     string
		config   *DuplexResilienceConfig
		expected int
	}{
		{
			name:     "nil config returns default",
			config:   nil,
			expected: defaultVal,
		},
		{
			name:     "zero value returns default",
			config:   &DuplexResilienceConfig{RetryDelayMs: 0},
			expected: defaultVal,
		},
		{
			name:     "positive value returns configured value",
			config:   &DuplexResilienceConfig{RetryDelayMs: 500},
			expected: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetRetryDelayMs(defaultVal)
			if got != tt.expected {
				t.Errorf("GetRetryDelayMs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDuplexResilienceConfig_GetInterTurnDelayMs(t *testing.T) {
	defaultVal := 200
	tests := []struct {
		name     string
		config   *DuplexResilienceConfig
		expected int
	}{
		{
			name:     "nil config returns default",
			config:   nil,
			expected: defaultVal,
		},
		{
			name:     "zero value returns default",
			config:   &DuplexResilienceConfig{InterTurnDelayMs: 0},
			expected: defaultVal,
		},
		{
			name:     "positive value returns configured value",
			config:   &DuplexResilienceConfig{InterTurnDelayMs: 500},
			expected: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetInterTurnDelayMs(defaultVal)
			if got != tt.expected {
				t.Errorf("GetInterTurnDelayMs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDuplexResilienceConfig_GetSelfplayInterTurnDelayMs(t *testing.T) {
	defaultVal := 1000
	tests := []struct {
		name     string
		config   *DuplexResilienceConfig
		expected int
	}{
		{
			name:     "nil config returns default",
			config:   nil,
			expected: defaultVal,
		},
		{
			name:     "zero value returns default",
			config:   &DuplexResilienceConfig{SelfplayInterTurnDelayMs: 0},
			expected: defaultVal,
		},
		{
			name:     "positive value returns configured value",
			config:   &DuplexResilienceConfig{SelfplayInterTurnDelayMs: 2000},
			expected: 2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetSelfplayInterTurnDelayMs(defaultVal)
			if got != tt.expected {
				t.Errorf("GetSelfplayInterTurnDelayMs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDuplexResilienceConfig_GetPartialSuccessMinTurns(t *testing.T) {
	defaultVal := 2
	tests := []struct {
		name     string
		config   *DuplexResilienceConfig
		expected int
	}{
		{
			name:     "nil config returns default",
			config:   nil,
			expected: defaultVal,
		},
		{
			name:     "zero value returns default",
			config:   &DuplexResilienceConfig{PartialSuccessMinTurns: 0},
			expected: defaultVal,
		},
		{
			name:     "positive value returns configured value",
			config:   &DuplexResilienceConfig{PartialSuccessMinTurns: 3},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetPartialSuccessMinTurns(defaultVal)
			if got != tt.expected {
				t.Errorf("GetPartialSuccessMinTurns() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDuplexResilienceConfig_ShouldIgnoreLastTurnSessionEnd(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name       string
		config     *DuplexResilienceConfig
		defaultVal bool
		expected   bool
	}{
		{
			name:       "nil config returns default true",
			config:     nil,
			defaultVal: true,
			expected:   true,
		},
		{
			name:       "nil config returns default false",
			config:     nil,
			defaultVal: false,
			expected:   false,
		},
		{
			name:       "nil pointer returns default",
			config:     &DuplexResilienceConfig{IgnoreLastTurnSessionEnd: nil},
			defaultVal: true,
			expected:   true,
		},
		{
			name:       "true value returns true",
			config:     &DuplexResilienceConfig{IgnoreLastTurnSessionEnd: &trueVal},
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "false value returns false",
			config:     &DuplexResilienceConfig{IgnoreLastTurnSessionEnd: &falseVal},
			defaultVal: true,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ShouldIgnoreLastTurnSessionEnd(tt.defaultVal)
			if got != tt.expected {
				t.Errorf("ShouldIgnoreLastTurnSessionEnd() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestScenario_DuplexResilienceParsing(t *testing.T) {
	yamlContent := `
id: duplex-resilience-test
task_type: interviewer
description: "Test duplex resilience config parsing"
duplex:
  timeout: "10m"
  turn_detection:
    mode: vad
  resilience:
    max_retries: 3
    retry_delay_ms: 500
    inter_turn_delay_ms: 200
    selfplay_inter_turn_delay_ms: 1500
    partial_success_min_turns: 2
    ignore_last_turn_session_end: true
turns:
  - role: user
    content: "Hello"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if scenario.Duplex == nil {
		t.Fatal("Expected duplex config to be parsed")
	}

	resilience := scenario.Duplex.GetResilience()
	if resilience == nil {
		t.Fatal("Expected resilience config to be parsed")
	}

	if resilience.MaxRetries != 3 {
		t.Errorf("Expected max_retries 3, got %d", resilience.MaxRetries)
	}
	if resilience.RetryDelayMs != 500 {
		t.Errorf("Expected retry_delay_ms 500, got %d", resilience.RetryDelayMs)
	}
	if resilience.InterTurnDelayMs != 200 {
		t.Errorf("Expected inter_turn_delay_ms 200, got %d", resilience.InterTurnDelayMs)
	}
	if resilience.SelfplayInterTurnDelayMs != 1500 {
		t.Errorf("Expected selfplay_inter_turn_delay_ms 1500, got %d", resilience.SelfplayInterTurnDelayMs)
	}
	if resilience.PartialSuccessMinTurns != 2 {
		t.Errorf("Expected partial_success_min_turns 2, got %d", resilience.PartialSuccessMinTurns)
	}
	if resilience.IgnoreLastTurnSessionEnd == nil || !*resilience.IgnoreLastTurnSessionEnd {
		t.Error("Expected ignore_last_turn_session_end to be true")
	}
}

func TestRelevanceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *RelevanceConfig
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
			config:  &RelevanceConfig{},
			wantErr: false,
		},
		{
			name: "valid openai provider",
			config: &RelevanceConfig{
				Provider: "openai",
			},
			wantErr: false,
		},
		{
			name: "valid gemini provider",
			config: &RelevanceConfig{
				Provider: "gemini",
			},
			wantErr: false,
		},
		{
			name: "valid voyageai provider",
			config: &RelevanceConfig{
				Provider: "voyageai",
			},
			wantErr: false,
		},
		{
			name: "invalid provider",
			config: &RelevanceConfig{
				Provider: "invalid-provider",
			},
			wantErr: true,
			errMsg:  "relevance provider must be",
		},
		{
			name: "valid similarity threshold",
			config: &RelevanceConfig{
				SimilarityThreshold: 0.5,
			},
			wantErr: false,
		},
		{
			name: "similarity threshold too low",
			config: &RelevanceConfig{
				SimilarityThreshold: -0.1,
			},
			wantErr: true,
			errMsg:  "similarity_threshold must be between",
		},
		{
			name: "similarity threshold too high",
			config: &RelevanceConfig{
				SimilarityThreshold: 1.5,
			},
			wantErr: true,
			errMsg:  "similarity_threshold must be between",
		},
		{
			name: "valid query source last_user",
			config: &RelevanceConfig{
				QuerySource: "last_user",
			},
			wantErr: false,
		},
		{
			name: "valid query source last_n with count",
			config: &RelevanceConfig{
				QuerySource: "last_n",
				LastNCount:  5,
			},
			wantErr: false,
		},
		{
			name: "query source last_n without count",
			config: &RelevanceConfig{
				QuerySource: "last_n",
				LastNCount:  0,
			},
			wantErr: true,
			errMsg:  "last_n_count must be positive",
		},
		{
			name: "valid query source custom with query",
			config: &RelevanceConfig{
				QuerySource: "custom",
				CustomQuery: "my custom query",
			},
			wantErr: false,
		},
		{
			name: "query source custom without query",
			config: &RelevanceConfig{
				QuerySource: "custom",
				CustomQuery: "",
			},
			wantErr: true,
			errMsg:  "custom_query is required",
		},
		{
			name: "invalid query source",
			config: &RelevanceConfig{
				QuerySource: "invalid",
			},
			wantErr: true,
			errMsg:  "query_source must be",
		},
		{
			name: "negative min_recent_messages",
			config: &RelevanceConfig{
				MinRecentMessages: -1,
			},
			wantErr: true,
			errMsg:  "min_recent_messages must be non-negative",
		},
		{
			name: "valid min_recent_messages",
			config: &RelevanceConfig{
				MinRecentMessages: 5,
			},
			wantErr: false,
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

func TestScenario_ValidateWithRelevance(t *testing.T) {
	t.Run("scenario with invalid relevance config", func(t *testing.T) {
		scenario := &Scenario{
			ID: "test-scenario",
			ContextPolicy: &ContextPolicy{
				Relevance: &RelevanceConfig{
					Provider: "invalid-provider",
				},
			},
		}
		err := scenario.Validate()
		if err == nil {
			t.Error("Validate() expected error for invalid relevance provider")
		}
		if !containsStr(err.Error(), "relevance provider must be") {
			t.Errorf("Validate() error = %v, expected to contain 'relevance provider must be'", err)
		}
	})

	t.Run("scenario with valid relevance config", func(t *testing.T) {
		scenario := &Scenario{
			ID: "test-scenario",
			ContextPolicy: &ContextPolicy{
				Relevance: &RelevanceConfig{
					Provider:            "openai",
					SimilarityThreshold: 0.7,
				},
			},
		}
		err := scenario.Validate()
		if err != nil {
			t.Errorf("Validate() unexpected error: %v", err)
		}
	})
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

func TestScenario_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Scenario
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil scenario is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "empty scenario is valid",
			config:  &Scenario{},
			wantErr: false,
		},
		{
			name: "scenario with valid duplex config",
			config: &Scenario{
				ID: "test-scenario",
				Duplex: &DuplexConfig{
					Timeout: "10m",
				},
			},
			wantErr: false,
		},
		{
			name: "scenario with invalid duplex timeout",
			config: &Scenario{
				ID: "test-scenario",
				Duplex: &DuplexConfig{
					Timeout: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid duplex timeout",
		},
		{
			name: "scenario with invalid VAD config",
			config: &Scenario{
				ID: "test-scenario",
				Duplex: &DuplexConfig{
					TurnDetection: &TurnDetectionConfig{
						Mode: TurnDetectionModeVAD,
						VAD: &VADConfig{
							SilenceThresholdMs: -1,
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "silence_threshold_ms must be non-negative",
		},
		{
			name: "scenario with invalid TTS config in turn",
			config: &Scenario{
				ID: "test-scenario",
				Turns: []TurnDefinition{
					{
						Role: "user",
						TTS: &TTSConfig{
							// Provider is required
							Voice: "alloy",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "tts provider is required",
		},
		{
			name: "scenario with valid TTS config in turn",
			config: &Scenario{
				ID: "test-scenario",
				Turns: []TurnDefinition{
					{
						Role: "user",
						TTS: &TTSConfig{
							Provider: "openai",
							Voice:    "alloy",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
				} else if !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %q", err, tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}
