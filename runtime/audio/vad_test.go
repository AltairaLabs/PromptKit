package audio

import (
	"testing"
)

func TestVADState_String(t *testing.T) {
	tests := []struct {
		state VADState
		want  string
	}{
		{VADStateQuiet, "quiet"},
		{VADStateStarting, "starting"},
		{VADStateSpeaking, "speaking"},
		{VADStateStopping, "stopping"},
		{VADState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("VADState.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultVADParams(t *testing.T) {
	params := DefaultVADParams()

	if params.Confidence != 0.5 {
		t.Errorf("Confidence = %v, want 0.5", params.Confidence)
	}
	if params.StartSecs != 0.2 {
		t.Errorf("StartSecs = %v, want 0.2", params.StartSecs)
	}
	if params.StopSecs != 0.8 {
		t.Errorf("StopSecs = %v, want 0.8", params.StopSecs)
	}
	if params.MinVolume != 0.01 {
		t.Errorf("MinVolume = %v, want 0.01", params.MinVolume)
	}
	if params.SampleRate != 16000 {
		t.Errorf("SampleRate = %v, want 16000", params.SampleRate)
	}
}

func TestVADParams_Validate(t *testing.T) {
	tests := []struct {
		name     string
		params   VADParams
		wantErr  bool
		errField string
	}{
		{
			name:    "valid default params",
			params:  DefaultVADParams(),
			wantErr: false,
		},
		{
			name:     "confidence too low",
			params:   VADParams{Confidence: -0.1, StartSecs: 0.2, StopSecs: 0.8, MinVolume: 0.01, SampleRate: 16000},
			wantErr:  true,
			errField: "Confidence",
		},
		{
			name:     "confidence too high",
			params:   VADParams{Confidence: 1.5, StartSecs: 0.2, StopSecs: 0.8, MinVolume: 0.01, SampleRate: 16000},
			wantErr:  true,
			errField: "Confidence",
		},
		{
			name:     "negative start secs",
			params:   VADParams{Confidence: 0.5, StartSecs: -1, StopSecs: 0.8, MinVolume: 0.01, SampleRate: 16000},
			wantErr:  true,
			errField: "StartSecs",
		},
		{
			name:     "negative stop secs",
			params:   VADParams{Confidence: 0.5, StartSecs: 0.2, StopSecs: -1, MinVolume: 0.01, SampleRate: 16000},
			wantErr:  true,
			errField: "StopSecs",
		},
		{
			name:     "min volume too high",
			params:   VADParams{Confidence: 0.5, StartSecs: 0.2, StopSecs: 0.8, MinVolume: 1.5, SampleRate: 16000},
			wantErr:  true,
			errField: "MinVolume",
		},
		{
			name:     "zero sample rate",
			params:   VADParams{Confidence: 0.5, StartSecs: 0.2, StopSecs: 0.8, MinVolume: 0.01, SampleRate: 0},
			wantErr:  true,
			errField: "SampleRate",
		},
		{
			name:     "negative sample rate",
			params:   VADParams{Confidence: 0.5, StartSecs: 0.2, StopSecs: 0.8, MinVolume: 0.01, SampleRate: -16000},
			wantErr:  true,
			errField: "SampleRate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("VADParams.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errField != "" {
				if ve, ok := err.(*ValidationError); ok {
					if ve.Field != tt.errField {
						t.Errorf("VADParams.Validate() error field = %v, want %v", ve.Field, tt.errField)
					}
				} else {
					t.Errorf("expected ValidationError, got %T", err)
				}
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{Field: "TestField", Message: "test message"}
	want := "invalid TestField: test message"
	if got := err.Error(); got != want {
		t.Errorf("ValidationError.Error() = %v, want %v", got, want)
	}
}
