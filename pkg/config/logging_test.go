package config

import (
	"strings"
	"testing"
)

func TestDefaultLoggingConfig(t *testing.T) {
	cfg := DefaultLoggingConfig()

	if cfg.DefaultLevel != LogLevelInfo {
		t.Errorf("DefaultLevel: expected %s, got %s", LogLevelInfo, cfg.DefaultLevel)
	}
	if cfg.Format != LogFormatText {
		t.Errorf("Format: expected %s, got %s", LogFormatText, cfg.Format)
	}
}

func TestLoggingConfigSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LoggingConfigSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: LoggingConfigSpec{
				DefaultLevel: LogLevelDebug,
				Format:       LogFormatJSON,
				Modules: []ModuleLoggingConfig{
					{Name: "runtime", Level: LogLevelDebug},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty config is valid",
			cfg:     LoggingConfigSpec{},
			wantErr: false,
		},
		{
			name: "invalid default level",
			cfg: LoggingConfigSpec{
				DefaultLevel: "invalid",
			},
			wantErr: true,
			errMsg:  "defaultLevel",
		},
		{
			name: "invalid format",
			cfg: LoggingConfigSpec{
				Format: "xml",
			},
			wantErr: true,
			errMsg:  "format",
		},
		{
			name: "module without name",
			cfg: LoggingConfigSpec{
				Modules: []ModuleLoggingConfig{
					{Level: LogLevelDebug},
				},
			},
			wantErr: true,
			errMsg:  "module name is required",
		},
		{
			name: "module with invalid level",
			cfg: LoggingConfigSpec{
				Modules: []ModuleLoggingConfig{
					{Name: "runtime", Level: "invalid"},
				},
			},
			wantErr: true,
			errMsg:  "modules[runtime].level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" {
					if ve, ok := err.(*ValidationError); ok {
						if ve.Field != tt.errMsg && ve.Message != tt.errMsg {
							// Check if error message contains expected string
							if !strings.Contains(err.Error(), tt.errMsg) {
								t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
							}
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ValidationError
		expected string
	}{
		{
			name: "with value",
			err: ValidationError{
				Field:   "format",
				Message: "must be json or text",
				Value:   "xml",
			},
			expected: "logging config validation error: format: must be json or text (got: xml)",
		},
		{
			name: "without value",
			err: ValidationError{
				Field:   "modules[0].name",
				Message: "module name is required",
			},
			expected: "logging config validation error: modules[0].name: module name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsValidLogLevel(t *testing.T) {
	validLevels := []string{"trace", "debug", "info", "warn", "error"}
	for _, level := range validLevels {
		if !isValidLogLevel(level) {
			t.Errorf("expected %s to be valid", level)
		}
	}

	invalidLevels := []string{"", "TRACE", "verbose", "fatal", "panic"}
	for _, level := range invalidLevels {
		if isValidLogLevel(level) {
			t.Errorf("expected %s to be invalid", level)
		}
	}
}

func TestLoggingConfig_Structure(t *testing.T) {
	cfg := LoggingConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "LoggingConfig",
		Metadata: ObjectMeta{
			Name: "test",
		},
		Spec: LoggingConfigSpec{
			DefaultLevel: LogLevelInfo,
			Format:       LogFormatText,
			CommonFields: map[string]string{
				"service": "test",
			},
			Modules: []ModuleLoggingConfig{
				{
					Name:  "runtime",
					Level: LogLevelDebug,
					Fields: map[string]string{
						"component": "core",
					},
				},
			},
		},
	}

	if cfg.APIVersion != "promptkit.altairalabs.ai/v1alpha1" {
		t.Errorf("unexpected APIVersion: %s", cfg.APIVersion)
	}
	if cfg.Kind != "LoggingConfig" {
		t.Errorf("unexpected Kind: %s", cfg.Kind)
	}
	if cfg.Metadata.Name != "test" {
		t.Errorf("unexpected Metadata.Name: %s", cfg.Metadata.Name)
	}
	if len(cfg.Spec.Modules) != 1 {
		t.Errorf("unexpected number of modules: %d", len(cfg.Spec.Modules))
	}
}
